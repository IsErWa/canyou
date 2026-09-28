package main

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// virtualAdapterKeywords 常见虚拟网卡名称关键字（VPN/WSL/Hyper-V/代理类网卡）
var virtualAdapterKeywords = []string{
	"vethernet", "hyper-v", "wsl", "tap", "tunnel",
	"virtual", "vmnet", "loopback", "npcap", "zanata",
}

// ScanLANIPv4Addresses 扫描本机所有处于启用状态的网卡，返回过滤后的局域网 IPv4 地址（已排序）
func ScanLANIPv4Addresses() ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("扫描网卡失败: %w", err)
	}

	var addresses []string
	for _, iface := range interfaces {
		if !isActiveInterface(iface) {
			continue
		}
		addresses = append(addresses, lanIPv4AddressesOf(iface)...)
	}
	sort.Strings(addresses)
	return addresses, nil
}

// isActiveInterface 判断网卡是否启用且不是虚拟网卡
func isActiveInterface(iface net.Interface) bool {
	if iface.Flags&net.FlagUp == 0 {
		return false
	}
	if iface.Flags&net.FlagLoopback != 0 {
		return false
	}
	return !isVirtualAdapterName(iface.Name)
}

// lanIPv4AddressesOf 获取网卡上全部非回环、非链路本地的 IPv4 地址字符串
func lanIPv4AddressesOf(iface net.Interface) []string {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}

	var addresses []string
	for _, addr := range addrs {
		ipNetwork, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNetwork.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		addresses = append(addresses, ip.String())
	}
	return addresses
}

// isVirtualAdapterName 按名称关键字识别虚拟网卡；
// GoLand 为 Hyper-V 默认交换机创建的网卡命名格式固定为 "vEthernet (...)"
func isVirtualAdapterName(name string) bool {
	lowerName := strings.ToLower(name)
	for _, keyword := range virtualAdapterKeywords {
		if strings.Contains(lowerName, keyword) {
			return true
		}
	}
	return false
}
