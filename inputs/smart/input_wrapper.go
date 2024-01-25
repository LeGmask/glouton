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
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs/smart"
)

const sgDevicesPattern = "/dev/sg[0-9]*"

type inputWrapper struct {
	*smart.Smart
	runCmd        runCmdType
	findSGDevices func() ([]string, error)

	allowedDevices []string
	// '/dev/sg_' devices are only listed at startup,
	// then reused at each gathering.
	sgDevices []string

	l sync.Mutex
}

type inputWrapperOptions struct {
	input         *smart.Smart
	runCmd        runCmdType
	findSGDevices func() ([]string, error)
}

func newInputWrapper(opts inputWrapperOptions) (*inputWrapper, error) {
	iw := &inputWrapper{
		Smart:          opts.input,
		allowedDevices: slices.Clone(opts.input.Devices), // We may update input.Devices at each gather
	}

	if opts.runCmd != nil {
		iw.runCmd = opts.runCmd
	} else {
		iw.runCmd = runCmd
	}

	if opts.findSGDevices != nil {
		iw.findSGDevices = opts.findSGDevices
	} else {
		iw.findSGDevices = func() ([]string, error) { return filepath.Glob(sgDevicesPattern) }
	}

	if len(iw.allowedDevices) != 0 {
		return iw, nil
	}

	devices, err := iw.getDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to scan devices: %w", err)
	}

	ignoreSGDevices := false

	for _, device := range devices {
		info, err := iw.getDeviceInfo(device)
		if err != nil {
			return nil, err
		}

		if shouldIgnoreDevice(info) {
			continue
		}

		ignoreSGDevices = true

		break
	}

	if !ignoreSGDevices { // smartctl scan gave no results, trying to find /dev/sg_ devices ...
		sgDevices, err := iw.findSGDevices()
		if err != nil {
			return nil, fmt.Errorf("failed to detect /dev/sg_ devices: %w", err)
		}

		for _, sgDev := range sgDevices {
			info, err := iw.getDeviceInfo(sgDev)
			if err != nil {
				return nil, err
			}

			if shouldIgnoreDevice(info) {
				continue
			}

			iw.sgDevices = append(iw.sgDevices, sgDev)
		}
	}

	return iw, nil
}

func (iw *inputWrapper) Gather(acc telegraf.Accumulator) error {
	iw.l.Lock()
	defer iw.l.Unlock()

	devices, err := iw.getDevices()
	if err != nil {
		return err
	}

	iw.Smart.Devices = devices

	return iw.Smart.Gather(acc)
}

func (iw *inputWrapper) getDevices() ([]string, error) {
	if len(iw.allowedDevices) != 0 {
		return iw.allowedDevices, nil
	}

	out, err := iw.runCmd(iw.Smart.Timeout, iw.Smart.UseSudo, iw.PathSmartctl, "--scan")
	if err != nil {
		return nil, err
	}

	return iw.parseScanOutput(out), nil
}

func (iw *inputWrapper) parseScanOutput(out []byte) (devices []string) {
	for _, line := range strings.Split(string(out), "\n") {
		devWithType := strings.SplitN(line, " ", 2)
		if len(devWithType) <= 1 {
			continue
		}

		if dev := strings.TrimSpace(devWithType[0]); iw.isDeviceAllowed(dev) {
			device := dev + deviceTypeFor(devWithType[1])
			devices = append(devices, device)
		}
	}

	devices = append(devices, iw.sgDevices...)

	return devices
}

func (iw *inputWrapper) getDeviceInfo(device string) (deviceInfo, error) {
	infoArgs := []string{"--info", "--health", "--attributes", "--tolerance=verypermissive", "-n", "standby", "--format=brief"}
	infoArgs = append(infoArgs, strings.Split(device, " ")...)

	infoOut, err := iw.runCmd(iw.Smart.Timeout, iw.Smart.UseSudo, iw.PathSmartctl, infoArgs...)
	if err != nil {
		return deviceInfo{}, fmt.Errorf("failed to get info about device %q: %w", device, err)
	}

	return iw.parseInfoOutput(infoOut), nil
}

func (iw *inputWrapper) parseInfoOutput(out []byte) deviceInfo {
	var info deviceInfo

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)

		if value, ok := tryScan(line, "Device type: %s"); ok {
			info.deviceType = value
		} else if value, ok = tryScan(line, "SMART support is: %s"); ok {
			info.smartSupport = value
		} else if value, ok = tryScan(line, "SMART overall-health self-assessment test result: %s"); ok {
			info.overallHealthTest = value
		}
	}

	return info
}

func tryScan(line string, format string) (value string, ok bool) {
	_, err := fmt.Sscanf(line, format, &value)
	if err != nil {
		return "", false
	}

	return value, true
}

func (iw *inputWrapper) isDeviceAllowed(device string) bool {
	if len(iw.allowedDevices) != 0 {
		for _, dev := range iw.allowedDevices {
			if device == dev {
				return true
			}
		}

		return false
	}

	for _, excluded := range iw.Smart.Excludes {
		if device == excluded {
			return false
		}
	}

	return true
}

func deviceTypeFor(devType string) string {
	if !strings.HasPrefix(devType, "-d ") {
		return ""
	}

	// Preventing some device types to be specified
	switch typ := strings.Split(devType, " ")[1]; typ {
	case "":
		return ""
	case "scsi":
		return ""
	default:
		return " -d " + typ
	}
}

type deviceInfo struct {
	deviceType        string
	smartSupport      string
	overallHealthTest string
}

func shouldIgnoreDevice(info deviceInfo) bool {
	switch {
	case strings.Contains(info.deviceType, "CD/DVD"):
		return true
	case (strings.Contains(info.smartSupport, "Unavailable") ||
		!(strings.Contains(info.smartSupport, "Available") || strings.Contains(info.smartSupport, "Enabled"))) &&
		!strings.Contains(info.overallHealthTest, "PASSED"):
		return true
	default:
		return false
	}
}
