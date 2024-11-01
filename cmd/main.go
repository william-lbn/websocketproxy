package main

func main() {
	backendAddress := "http://172.24.79.116:32714/hyperos/cloudshell/nginx-ingress-demolbn" // 替换为后端服务的地址
	proxy := NewProxy(backendAddress)                                                       // 创建代理实例
	proxy.Start(":8080")                                                                    // 启动代理服务器
}
