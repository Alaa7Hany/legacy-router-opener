package gateway

import (
	"testing"
)

func TestGatewayParsers(t *testing.T) {
	winRoute := `
IPv4 Route Table
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0         On-link       10.10.0.2     45
          0.0.0.0          0.0.0.0      192.168.8.1    192.168.8.2     35
          0.0.0.0          0.0.0.0      192.168.1.1    192.168.1.5     25
===========================================================================`
	if got := ParseWindowsRoutePrint(winRoute); got != "192.168.1.1" {
		t.Fatalf("ParseWindowsRoutePrint = %q, want 192.168.1.1", got)
	}

	winIPConfig := `
Ethernet adapter Ethernet:
   IPv4 Address. . . . . . . . . . . : 192.168.8.101
   Default Gateway . . . . . . . . . :
Wireless LAN adapter Wi-Fi:
   Default Gateway . . . . . . . . . : 192.168.100.1`
	if got := ParseWindowsIPConfig(winIPConfig); got != "192.168.100.1" {
		t.Fatalf("ParseWindowsIPConfig = %q, want 192.168.100.1", got)
	}

	procRoute := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"wlan0\t00000000\tFE01A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n"
	if got := ParseProcNetRoute(procRoute); got != "192.168.1.1" {
		t.Fatalf("ParseProcNetRoute = %q, want 192.168.1.1", got)
	}

	if got := ParseLinuxIPRoute("default via 10.20.30.1 dev wlan0 proto dhcp metric 600"); got != "10.20.30.1" {
		t.Fatalf("ParseLinuxIPRoute = %q, want 10.20.30.1", got)
	}

	macRoute := "   route to: default\n destination: default\n     gateway: 172.16.0.1\n"
	if got := ParseMacRouteGet(macRoute); got != "172.16.0.1" {
		t.Fatalf("ParseMacRouteGet = %q, want 172.16.0.1", got)
	}
}

func TestWindowsGatewaysAtOtherAddresses(t *testing.T) {
	for _, tc := range []struct {
		name, route, ipconfig, want string
	}{
		{
			name: "192.168.0.1",
			route: "Active Routes:\n" +
				"0.0.0.0 0.0.0.0 192.168.1.1 192.168.1.23 55\n" +
				"0.0.0.0 0.0.0.0 192.168.0.1 192.168.0.23 15\n",
			ipconfig: "Wireless LAN adapter Wi-Fi:\r\n" +
				"   IPv4 Address. . . . . . . . . . . : 192.168.0.23\r\n" +
				"   Default Gateway . . . . . . . . . : 192.168.0.1\r\n",
			want: "192.168.0.1",
		},
		{
			name: "10.0.0.1",
			route: "Active Routes:\n" +
				"0.0.0.0 0.0.0.0 On-link 10.64.0.2 10\n" +
				"0.0.0.0 0.0.0.0 10.0.0.1 10.0.0.42 25\n",
			ipconfig: "Ethernet adapter Ethernet:\r\n" +
				"   IPv4 Address. . . . . . . . . . . : 10.0.0.42\r\n" +
				"   Default Gateway . . . . . . . . . : 10.0.0.1\r\n",
			want: "10.0.0.1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseWindowsRoutePrint(tc.route); got != tc.want {
				t.Errorf("route table identifies %q, want %q", got, tc.want)
			}
			if got := ParseWindowsIPConfig(tc.ipconfig); got != tc.want {
				t.Errorf("ipconfig identifies %q, want %q", got, tc.want)
			}
		})
	}
}
