package proxy

import (
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"sync"
	"time"
)

// WebSocket 升级器，用于将 HTTP 请求升级为 WebSocket 连接
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源的请求
	},
}

// Proxy 结构体，用于管理连接和状态
type Proxy struct {
	connections    map[*websocket.Conn]time.Time // 存储客户端连接及其最后活动时间
	lock           sync.Mutex                    // 用于并发访问的锁，确保线程安全
	timeout        time.Duration                 // 空闲超时时间
	backendAddress string                        // 后端服务的 WebSocket 地址
	backendConn    *websocket.Conn               // 与后端服务的连接
}

// NewProxy 创建一个新的 Proxy 实例
func NewProxy(backendAddress string) *Proxy {
	return &Proxy{
		connections:    make(map[*websocket.Conn]time.Time), // 初始化连接映射
		backendAddress: backendAddress,                      // 设置后端服务地址
		timeout:        30 * time.Second,                    // 设置超时时间
	}
}

// connectToBackend 建立与后端服务的 WebSocket 连接
func (p *Proxy) connectToBackend() (*websocket.Conn, error) {
	// 使用默认的拨号器建立连接
	conn, _, err := websocket.DefaultDialer.Dial(p.backendAddress, nil)
	return conn, err // 返回连接和可能的错误
}

// handleConnection 处理新的客户端连接
func (p *Proxy) handleConnection(w http.ResponseWriter, r *http.Request) {
	// 将 HTTP 升级为 WebSocket 连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Error while upgrading connection:", err)
		return
	}
	defer conn.Close() // 确保连接在函数结束时关闭

	// 记录新的客户端连接及其最后活动时间
	p.lock.Lock()
	p.connections[conn] = time.Now()
	p.lock.Unlock()
	fmt.Printf("New connection established. Active connections: %d\n", len(p.connections))

	// 如果没有与后端的连接，则建立连接
	p.lock.Lock()
	if p.backendConn == nil {
		backendConn, err := p.connectToBackend() // 建立与后端的连接
		if err != nil {
			fmt.Println("Error connecting to backend:", err)
			conn.Close() // 关闭客户端连接
			p.lock.Unlock()
			return
		}
		p.backendConn = backendConn // 记录后端连接
		p.lock.Unlock()

		// 启动一个协程来读取后端服务的消息并转发到客户端
		go p.forwardMessages(backendConn, conn)

		// 启动一个协程来监控后端连接的状态
		go p.monitorBackendConnection()
	} else {
		p.lock.Unlock()
	}

	// 启动一个协程来读取来自客户端的消息并转发到后端服务
	go func() {
		for {
			_, msg, err := conn.ReadMessage() // 读取客户端消息
			if err != nil {
				fmt.Println("Client connection closed:", err)
				break // 读取错误，结束循环
			}
			// 更新最后活跃时间
			p.lock.Lock()
			p.connections[conn] = time.Now()
			p.lock.Unlock()

			// 转发消息到后端服务
			if err := p.backendConn.WriteMessage(websocket.TextMessage, msg); err != nil {
				fmt.Println("Error sending message to backend:", err)
				break // 转发错误，结束循环
			}
		}
	}()

	// 定期检查空闲连接和后端连接状态
	for {
		time.Sleep(5 * time.Second) // 每 5 秒检查一次
		p.lock.Lock()
		// 检查每个客户端连接的活跃状态
		for c, lastActive := range p.connections {
			if time.Since(lastActive) > p.timeout { // 判断是否超时
				fmt.Println("Closing connection due to inactivity")
				c.Close()                // 关闭空闲连接
				delete(p.connections, c) // 从映射中删除连接
			}
		}
		fmt.Printf("Active connections: %d\n", len(p.connections))

		// 如果没有任何客户端连接，则关闭与后端的连接
		if len(p.connections) == 0 && p.backendConn != nil {
			fmt.Println("No active client connections. Closing backend connection.")
			p.backendConn.Close() // 关闭后端连接
			p.backendConn = nil   // 重置后端连接
		}
		p.lock.Unlock()
	}
}

// forwardMessages 从后端服务读取消息并转发给客户端
func (p *Proxy) forwardMessages(backendConn, clientConn *websocket.Conn) {
	for {
		_, msg, err := backendConn.ReadMessage() // 读取后端服务消息
		if err != nil {
			fmt.Println("Backend connection closed:", err)
			break // 读取错误，结束循环
		}

		// 转发消息到客户端
		if err := clientConn.WriteMessage(websocket.TextMessage, msg); err != nil {
			fmt.Println("Error sending message to client:", err)
			break // 转发错误，结束循环
		}
	}
}

// monitorBackendConnection 定期检查与后端的连接状态
func (p *Proxy) monitorBackendConnection() {
	for {
		time.Sleep(10 * time.Second) // 每 10 秒检查一次
		p.lock.Lock()
		// 如果后端连接已关闭，则尝试重新连接
		if p.backendConn == nil {
			fmt.Println("Reconnecting to backend service...")
			backendConn, err := p.connectToBackend()
			if err != nil {
				fmt.Println("Error reconnecting to backend:", err) // 连接错误
			} else {
				p.backendConn = backendConn // 记录新的后端连接
			}
		}
		p.lock.Unlock()
	}
}

// Start 启动 WebSocket 代理服务器
func (p *Proxy) Start(address string) {
	http.HandleFunc("/ws", p.handleConnection) // 注册处理函数
	fmt.Printf("WebSocket proxy server started on %s\n", address)
	if err := http.ListenAndServe(address, nil); err != nil {
		fmt.Println("Error starting server:", err) // 启动服务器时出错
	}
}
