// Copyright 2015-2023 Bleemeo
//
// bleemeo.com an infrastructure monitoring solution in the Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package smart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs/smart"
	"gopkg.in/yaml.v3"
)

var errInvalidArguments = errors.New("invalid arguments")

func TestStorageDevicesPattern(t *testing.T) {
	if _, err := filepath.Match(sgDevicesPattern, "foo"); err != nil {
		t.Fatalf("Storage devices pattern is invalid: %v", err)
	}
}

func TestDeviceTypeFor(t *testing.T) {
	input2Expected := map[string]string{
		"nvme":          "",
		"-d nvme":       " -d nvme",
		"-d scsi":       "",
		"-d megaraid,7": " -d megaraid,7",
		"# unexpected":  "",
	}

	for input, expected := range input2Expected {
		result := deviceTypeFor(input)
		if result != expected {
			t.Errorf("Invalid result from deviceTypeFor(%q): want %q, got %q", input, expected, result)
		}
	}
}

func TestIsDeviceAllowed(t *testing.T) {
	wrapper := inputWrapper{
		Smart: &smart.Smart{
			Excludes: []string{"/dev/nvme1"},
		},
	}

	expectedOutput := map[string]bool{
		"/dev/nvme0": true,
		"/dev/nvme1": false,
		"/dev/nvme2": true,
	}
	output := make(map[string]bool)

	for device := range expectedOutput {
		result := wrapper.isDeviceAllowed(device)
		output[device] = result
	}

	if diff := cmp.Diff(expectedOutput, output); diff != "" {
		t.Fatalf("Unexpected device filtering (-want +got):\n%s", diff)
	}
}

func TestParseScanOutput(t *testing.T) {
	testCases := []struct {
		name                            string
		noDataFile                      bool
		configDevices                   []string
		sgDevices                       []string
		expectedInitSmartctlInvocations int
		expectedDevices                 []string
		expectedToIgnoreStorageDevices  bool
		expectedScanSmartctlInvocations int
	}{
		{
			name:                            "firewall",
			sgDevices:                       []string{"/dev/sg0", "/dev/sg1", "/dev/sg2", "/dev/sg3"},
			expectedInitSmartctlInvocations: 6, // 1 scan + 1 info /dev/sda + 4 info /dev/sg_
			//                               /dev/sda is unusable, but the telegraf input with deal with it.
			expectedDevices:                 []string{"/dev/sda", "/dev/sg2", "/dev/sg3"},
			expectedToIgnoreStorageDevices:  false,
			expectedScanSmartctlInvocations: 1,
		},
		{
			name:                            "home1",
			expectedInitSmartctlInvocations: 2,
			expectedDevices:                 []string{"/dev/sda", "/dev/sdb", "/dev/nvme0 -d nvme"},
			expectedToIgnoreStorageDevices:  true,
			expectedScanSmartctlInvocations: 1,
		},
		{
			name:                            "home2",
			sgDevices:                       []string{"/dev/sg0"},
			expectedInitSmartctlInvocations: 2,
			expectedDevices:                 []string{"/dev/sda"},
			expectedToIgnoreStorageDevices:  true,
			expectedScanSmartctlInvocations: 1,
		},
		{
			name:                            "macOS",
			expectedInitSmartctlInvocations: 2,
			expectedDevices:                 []string{"IOService:/AppleARMPE/arm-io/AppleT600xIO/ans@8F400000/AppleASCWrapV4/iop-ans-nub/RTBuddy(ANS2)/RTBuddyService/AppleANS3NVMeController/NS_01@1 -d nvme"}, //nolint:lll
			expectedToIgnoreStorageDevices:  true,
			expectedScanSmartctlInvocations: 1,
		},
		{
			name:                            "proxmox1",
			sgDevices:                       []string{"/dev/sg0"},
			expectedInitSmartctlInvocations: 3,
			expectedDevices:                 []string{"/dev/sda", "/dev/bus/0 -d megaraid,0", "/dev/bus/0 -d megaraid,1", "/dev/bus/0 -d megaraid,2", "/dev/bus/0 -d megaraid,3", "/dev/bus/0 -d megaraid,4", "/dev/bus/0 -d megaraid,5", "/dev/bus/0 -d megaraid,6", "/dev/bus/0 -d megaraid,7", "/dev/bus/0 -d megaraid,8", "/dev/bus/0 -d megaraid,9", "/dev/bus/0 -d megaraid,10", "/dev/bus/0 -d megaraid,11", "/dev/bus/0 -d megaraid,12", "/dev/bus/0 -d megaraid,13"}, //nolint:lll
			expectedToIgnoreStorageDevices:  true,
			expectedScanSmartctlInvocations: 1,
		},
		{
			name:                            "proxmox2",
			sgDevices:                       []string{"/dev/sg0", "/dev/sg1"},
			expectedInitSmartctlInvocations: 3,
			expectedDevices:                 []string{"/dev/sda", "/dev/bus/0 -d megaraid,0", "/dev/bus/0 -d megaraid,1"},
			expectedToIgnoreStorageDevices:  true,
			expectedScanSmartctlInvocations: 1,
		},
		{
			name:                            "config-devices",
			noDataFile:                      true,
			configDevices:                   []string{"/dev/sdf"},
			expectedInitSmartctlInvocations: 0,
			expectedDevices:                 []string{"/dev/sdf"},
			expectedScanSmartctlInvocations: 0,
		},
	}

	for _, testCase := range testCases {
		tc := testCase

		t.Run(tc.name, func(t *testing.T) {
			var (
				smartctlData SmartctlData
				err          error
			)

			if !tc.noDataFile {
				smartctlData, err = parseSmartctlData(tc.name)
				if err != nil {
					t.Error("Failed to parse smartctl data:", err)

					return
				}
			}

			opts := inputWrapperOptions{
				input: &smart.Smart{
					Devices: tc.configDevices,
				},
				runCmd: smartctlData.makeRunCmdFor(t),
				findSGDevices: func() ([]string, error) {
					return tc.sgDevices, nil
				},
			}

			iw, err := newInputWrapper(opts)
			if err != nil {
				t.Error("Can't initialize SMART input wrapper:", err)

				return
			}

			if invocCount := smartctlData.invocationsCount; invocCount != tc.expectedInitSmartctlInvocations {
				t.Errorf("Expected smartctl to be invocated %d times at init, but was %d times.", tc.expectedInitSmartctlInvocations, invocCount)
			}

			smartctlData.invocationsCount = 0

			devices, err := iw.getDevices()
			if err != nil {
				t.Fatal("Failed to get devices:", err)
			}

			if diff := cmp.Diff(tc.expectedDevices, devices, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Unexpected devices (-want +got):\n%s", diff)
			}

			if invocCount := smartctlData.invocationsCount; invocCount != tc.expectedScanSmartctlInvocations {
				t.Errorf("Expected smartctl to be invocated %d times at scan, but was %d times.", tc.expectedScanSmartctlInvocations, invocCount)
			}
		})
	}
}

type SmartctlData struct {
	Scan string            `yaml:"scan"`
	Info map[string]string `yaml:"info"`

	invocationsCount int
}

func parseSmartctlData(inputName string) (SmartctlData, error) {
	raw, err := os.ReadFile("./testdata/" + inputName + ".yml")
	if err != nil {
		return SmartctlData{}, err
	}

	var smartctlData SmartctlData

	err = yaml.Unmarshal(raw, &smartctlData)
	if err != nil {
		return SmartctlData{}, err
	}

	return smartctlData, nil
}

func (smartctlData *SmartctlData) makeRunCmdFor(t *testing.T) runCmdType {
	t.Helper()

	const deviceNotFound = `smartctl 7.2 2020-12-30 r5155 [x86_64-linux-5.15.0-91-generic] (local build)
Copyright (C) 2002-20, Bruce Allen, Christian Franke, www.smartmontools.org

Smartctl open device: %s failed: No such device
`

	return func(_ config.Duration, _ bool, _ string, args ...string) ([]byte, error) {
		smartctlData.invocationsCount++

		switch cmd := args[0]; cmd {
		case "--scan":
			return []byte(smartctlData.Scan), nil
		case "--info":
		// Handling it below
		default:
			err := fmt.Errorf("%w: %v", errInvalidArguments, args)
			t.Error(err)

			return nil, err
		}

		var device string

		if args[len(args)-2] == "-d" {
			device = strings.Join(args[len(args)-3:], " ")
		} else {
			device = args[len(args)-1]
		}

		infoData, found := smartctlData.Info[device]
		if !found {
			t.Errorf("Info about device %q not found.", device)

			return []byte(fmt.Sprintf(deviceNotFound, device)), fmt.Errorf("%w: device %q not found", errInvalidArguments, device)
		}

		return []byte(infoData), nil
	}
}
