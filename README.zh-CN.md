# Port Forwarder（端口转发器）

一个轻量的 TCP 端口转发守护进程，自带嵌入式 Web 管理界面。你可以直接在浏览器里管理多条转发规则，无需编写任何配置文件。

盘古空格规范：本 README（简体中文版）遵循 [盘古空格](https://github.com/vinta/pangu) 的排版规则，中英文之间添加空格。

## 特性

- 纯 TCP 转发，暂无加密与认证（后续版本补充）
- 嵌入式 Web 管理界面，零外部静态资源
- 提供 REST API 用于规则管理
- 单静态二进制，可交叉编译到 Linux 和 Windows

## 构建

需要 Go 1.25 及以上。

```sh
make all        # 生成 bin/port-forwarder-{linux,windows}-amd64(.exe)
make test       # 运行测试套件
```

## 使用方法

```sh
./bin/port-forwarder-linux-amd64 -web :28774
```

然后在浏览器中打开 <http://localhost:28774/>，添加规则：

| 字段   | 含义                         | 示例                |
| ------ | ---------------------------- | ------------------- |
| name   | 规则的名称                   | `http-dev`          |
| listen | 本地监听的地址               | `:8080`             |
| target | 所有连接将被转发到的地址     | `127.0.0.1:3000`    |

默认 Web UI 端口 `28774` 由命令行名称推导而来：`pf` 的 ASCII 码为 `0x70 0x66`，拼接后为 `0x7066`，转换为十进制即 `28774`。

## 通过 SSH 反向转发

若要将 NAT 内的一台机器上的端口暴露到公网，且公网主机上不安装任何程序，可以使用 `ssh` 模式。其行为类似 `ssh -R`：`pf` 登录一台普通的 `sshd`，隧道在普通 SSH 会话内传输。

```sh
# 在 NAT 内网、你拥有控制权的设备上运行
./bin/port-forwarder-linux-amd64 ssh -host vps.example.com:22

# 或者指定密码，跳过 ssh-agent：
./bin/port-forwarder-linux-amd64 ssh -host vps.example.com -pass 'secret'
```

随后在 Web 管理界面中，把表单切换到“远程转发（ssh -R）”，添加一条规则，例如：

| 字段   | 含义                              | 示例                |
| ------ | --------------------------------- | ------------------- |
| listen | 公网服务器上开放的端口             | `:7788`             |
| target | NAT 内网的私有地址                | `192.168.1.2:7777`  |

此时访问 `vps.example.com:7788`，数据会通过 SSH 隧道到达内网的 `192.168.1.2:7777`。

### 公网主机的准备工作

公网主机只需运行标准 `sshd`，但有两个关键点：

- **GatewayPorts**——默认情况下，`ssh -R` 会把远程端口绑定在 `127.0.0.1` 上，公网无法访问。若要在公网接口上暴露该端口，请在 `/etc/ssh/sshd_config` 中设置 `GatewayPorts yes`，然后重启 sshd。（即使设置为 `no`，反向转发对回环连接仍然有效。）
- **凭据**——认证顺序为：先尝试 ssh-agent，再尝试 `-key`，最后是 `-pass`。

### 参数

| 参数          | 含义                          |
| ------------- | ----------------------------- |
| `-host`       | sshd 地址，例如 `vps.example.com:22` |
| `-user`       | SSH 登录用户                  |
| `-key`        | 私钥文件路径                  |
| `-passphrase` | 加密私钥的口令                |
| `-pass`       | SSH 登录密码（会禁用 agent 认证） |

## REST API

| 方法     | 路径                        | 说明                 |
| -------- | --------------------------- | ------------------- |
| GET      | `/api/rules`                | 列出所有规则         |
| POST     | `/api/rules`                | 创建规则             |
| DELETE   | `/api/rules/{id}`           | 删除规则             |
| POST     | `/api/rules/{id}/restart`   | 重启所有规则         |
| GET      | `/api/health`               | 健康检查             |

创建规则：

```sh
curl -X POST http://localhost:28774/api/rules \
  -H 'Content-Type: application/json' \
  -d '{"name":"http-dev","listen":":8080","target":"127.0.0.1:3000"}'
```

## 许可协议

MIT