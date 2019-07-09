package nrpe

import (
	"bytes"
	"io"
	"testing"
)

//request 1 = "check_load"
var request1 = []byte{0x0, 0x3, 0x0, 0x1, 0x95, 0x5d, 0xd9, 0xff, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x3, 0xf9, 0x63, 0x68, 0x65, 0x63, 0x6b, 0x5f, 0x6c, 0x6f, 0x61, 0x64, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}

//answer 1="WARNING - load average: 0.02, 0.07, 0.06|load1=0.022;0.150;0.300;0; load5=0.068;0.100;0.250;0; load15=0.060;0.050;0.200;0; "
var answer1 = []byte{0x00, 0x03, 0x00, 0x02, 0x1c, 0xec, 0xd5, 0x70,
	0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7b,
	0x57, 0x41, 0x52, 0x4e, 0x49, 0x4e, 0x47, 0x20,
	0x2d, 0x20, 0x6c, 0x6f, 0x61, 0x64, 0x20, 0x61,
	0x76, 0x65, 0x72, 0x61, 0x67, 0x65, 0x3a, 0x20,
	0x30, 0x2e, 0x30, 0x32, 0x2c, 0x20, 0x30, 0x2e,
	0x30, 0x37, 0x2c, 0x20, 0x30, 0x2e, 0x30, 0x36,
	0x7c, 0x6c, 0x6f, 0x61, 0x64, 0x31, 0x3d, 0x30,
	0x2e, 0x30, 0x32, 0x32, 0x3b, 0x30, 0x2e, 0x31,
	0x35, 0x30, 0x3b, 0x30, 0x2e, 0x33, 0x30, 0x30,
	0x3b, 0x30, 0x3b, 0x20, 0x6c, 0x6f, 0x61, 0x64,
	0x35, 0x3d, 0x30, 0x2e, 0x30, 0x36, 0x38, 0x3b,
	0x30, 0x2e, 0x31, 0x30, 0x30, 0x3b, 0x30, 0x2e,
	0x32, 0x35, 0x30, 0x3b, 0x30, 0x3b, 0x20, 0x6c,
	0x6f, 0x61, 0x64, 0x31, 0x35, 0x3d, 0x30, 0x2e,
	0x30, 0x36, 0x30, 0x3b, 0x30, 0x2e, 0x30, 0x35,
	0x30, 0x3b, 0x30, 0x2e, 0x32, 0x30, 0x30, 0x3b,
	0x30, 0x3b, 0x20, 0x00, 0x00, 0x00}

//answer 2 = "USERS OK - 1 users currently logged in |users=1;5;10;0"
var answer2 = []byte{0x00, 0x03, 0x00, 0x02, 0x30, 0x69, 0x30, 0xe6,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x36,
	0x55, 0x53, 0x45, 0x52, 0x53, 0x20, 0x4f, 0x4b,
	0x20, 0x2d, 0x20, 0x31, 0x20, 0x75, 0x73, 0x65,
	0x72, 0x73, 0x20, 0x63, 0x75, 0x72, 0x72, 0x65,
	0x6e, 0x74, 0x6c, 0x79, 0x20, 0x6c, 0x6f, 0x67,
	0x67, 0x65, 0x64, 0x20, 0x69, 0x6e, 0x20, 0x7c,
	0x75, 0x73, 0x65, 0x72, 0x73, 0x3d, 0x31, 0x3b,
	0x35, 0x3b, 0x31, 0x30, 0x3b, 0x30, 0x00, 0x00,
	0x00}

//request 2 = "check_user"
var request2 = []byte{0x00, 0x03, 0x00, 0x01, 0xbe, 0x3a, 0xf7, 0x85,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0xf9,
	0x63, 0x68, 0x65, 0x63, 0x6b, 0x5f, 0x75, 0x73,
	0x65, 0x72, 0x73, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00}
var request3 = []byte{0x00, 0x03, 0x00, 0x01, 0xa6, 0xef, 0xde, 0x8b,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x61,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x00, 0x00, 0x00, 0x00}
var answer4 = []byte{0x00, 0x03, 0x00, 0x02, 0x07, 0x50, 0x7f, 0xbc,
	0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x04, 0x60,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x61, 0x7a, 0x65, 0x72, 0x74, 0x79, 0x79, 0x75,
	0x69, 0x6f, 0x70, 0x61, 0x7a, 0x65, 0x74, 0x79,
	0x75, 0x69, 0x6f, 0x61, 0x65, 0x72, 0x74, 0x79,
	0x75, 0x70, 0x61, 0x7a, 0x65, 0x72, 0x70, 0x6f,
	0x61, 0x7a, 0x69, 0x65, 0x72, 0x70, 0x6f, 0x61,
	0x69, 0x7a, 0x75, 0x65, 0x72, 0x70, 0x61, 0x7a,
	0x69, 0x65, 0x75, 0x72, 0x61, 0x75, 0x72, 0x66,
	0x00, 0x00, 0x00}

const Answer = "azertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurfazertyyuiopazetyuioaertyupazerpoazierpoaizuerpazieuraurf"

func TestDecode(t *testing.T) {
	cases := []struct {
		in   io.Reader
		want reducedPacket
	}{
		{bytes.NewReader(request1), reducedPacket{1, 0, "check_load"}},
		{bytes.NewReader(request2), reducedPacket{1, 0, "check_users"}},
		{bytes.NewReader(request3), reducedPacket{1, 0, Answer}},
		{bytes.NewReader(answer4), reducedPacket{2, 1, Answer}},
	}
	for _, c := range cases {
		got, err := decode(c.in)
		if got != c.want {
			t.Errorf("decode(nrpePacket) == %v, want %v", got, c.want)
		}
		if err != nil {
			t.Error(err)
		}
	}
}

func TestDecodecrc32(t *testing.T) {
	nrpePacketCopy := make([]byte, len(request1))
	copy(nrpePacketCopy, request1)
	nrpePacketCopy[6] = 0x2d
	cases := []struct {
		in   io.Reader
		want reducedPacket
	}{
		{bytes.NewReader(nrpePacketCopy), reducedPacket{1, 0, "check_load"}},
	}
	for _, c := range cases {
		_, err := decode(c.in)
		if err == nil {
			t.Error("no error for crc32 value")
		}
	}
}

func TestDecodeEncodeV3(t *testing.T) {
	cases := []struct {
		in   reducedPacket
		want reducedPacket
	}{
		{reducedPacket{2, 0, "connection successful"}, reducedPacket{2, 0, "connection successful"}},
	}
	for _, c := range cases {
		inter, _ := encodeV3(c.in)
		got, err := decode(bytes.NewReader(inter))
		if got != c.want {
			t.Errorf("decode(encodeV3(%v)) == %v, want %v", c.in, got, c.want)
		}
		if err != nil {
			t.Error(err)
		}
	}
}

func TestEncodeV3(t *testing.T) {
	cases := []struct {
		in   reducedPacket
		want []byte
	}{
		{reducedPacket{1, 1, "WARNING - load average: 0.02, 0.07, 0.06|load1=0.022;0.150;0.300;0; load5=0.068;0.100;0.250;0; load15=0.060;0.050;0.200;0; "}, answer1},
		{reducedPacket{1, 0, "USERS OK - 1 users currently logged in |users=1;5;10;0"}, answer2},
		{reducedPacket{2, 1, Answer}, answer4},
	}
	for _, c := range cases {
		got, err := encodeV3(c.in)
		if len(got) != len(c.want) {
			t.Errorf("encodeV3(%v) == %v, want %v", c.in, got, c.want)
			break
		}
		for i := 0; i < len(got); i++ {
			if got[i] != c.want[i] {
				t.Errorf("encodeV3(%v) == %v, want %v", c.in, got, c.want)
				break
			}
		}
		if err != nil {
			t.Error(err)
		}
	}
}
