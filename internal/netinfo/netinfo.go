package netinfo

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

// LANIP 一个可共享的局域网 IPv4 地址及其所属网卡名
type LANIP struct {
	IP      string
	Adapter string
}

// ScanLANIPv4 扫描本机所有处于启用状态的网卡，返回过滤后的局域网 IPv4 地址与网卡名（按 IP 排序）
func ScanLANIPv4() ([]LANIP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("扫描网卡失败: %w", err)
	}

	var addresses []LANIP
	for _, iface := range interfaces {
		if !isActiveInterface(iface) {
			continue
		}
		for _, ip := range lanIPv4AddressesOf(iface) {
			addresses = append(addresses, LANIP{IP: ip, Adapter: iface.Name})
		}
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].IP < addresses[j].IP })
	return addresses, nil
}

// PreferredIPIndex 在刷新后的地址中保留原选择；原地址失效时回退首项，无地址时取消选择。
func PreferredIPIndex(entries []LANIP, preferredIP string) int {
	for index, entry := range entries {
		if entry.IP == preferredIP {
			return index
		}
	}
	if len(entries) == 0 {
		return -1
	}
	return 0
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
