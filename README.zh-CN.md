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