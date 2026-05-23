# 关键假设

| ID | 假设 |
|---|---|
| A1 | VPS 上的 claude code 已可用（登录 / 认证不在本项目范围） |
| A2 | Worker 机器上已 clone 好对应项目 |
| A3 | 单用户场景，无并发用户冲突 |
| A4 | 用户能通过 SSH 隧道访问 VPS 上的 Web Console loopback 端口（自管隧道）|
| A5 | 网络可达性：Worker → VPS 出站可达（公网 / 隧道二选一，由用户保证） |
