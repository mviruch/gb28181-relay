# gb28181-relay

纯 Go 国标 **假设备 / FLV→GB28181** 中继：多设备 REGISTER 到（可不同的）上级平台，通道源为 HTTP-FLV，媒体由 **ZLMediaKit HTTP API** 拉流并 `startSendRtp` / `startSendRtpPassive` 推 PS-RTP。本进程 **不自己推 RTP/PS**。

> 本机命令行运行；无网页 UI、无云端编排依赖。

## 首版能力

- 多设备 `REGISTER` + Digest
- 心跳 `Keepalive`（MESSAGE Notify）
- `Catalog` / `DeviceInfo` / `DeviceStatus` 查询应答（MANSCDP XML）
- 实时 `INVITE` 点播（Play）+ `BYE` 清理 ZLM 推流/代理
- 每设备可指向不同上级；每设备多通道；通道 = `channelId` + `flvUrl`

## 明确不做（首版）

- 回放 / 下载、云台 / PTZ、报警订阅
- 网页 UI、MySQL / Redis（仅 YAML + 内存会话）
- 本进程内 RTP/PS 收发（全部交给 ZLM）

## 依赖

| 组件 | 用途 |
|------|------|
| [emiago/sipgo](https://github.com/emiago/sipgo) | SIP UA（REGISTER / MESSAGE / INVITE） |
| [mickeyzzc/gb28181-go/manscdp](https://github.com/mickeyzzc/gb28181-go) | **仅** MANSCDP XML 编解码（未用其 `device/` 全量） |
| [ZLMediaKit](https://github.com/ZLMediaKit/ZLMediaKit) | `addStreamProxy` + `startSendRtp` / `startSendRtpPassive` + `stopSendRtp` |

Go 版本：需 **≥ 1.26**（上游 `manscdp` 模块要求）。

## 目录

```
cmd/relay/main.go
internal/config/     # YAML
internal/sipua/      # 每设备注册循环 + MESSAGE/INVITE/BYE
internal/catalog/    # Catalog/Keepalive/DeviceInfo 组装
internal/zlm/        # ZLM HTTP API 客户端
internal/session/    # 点播会话与清理
config.example.yaml
```

## 怎么跑

1. 部署并启动 ZLMediaKit，记下 HTTP API 地址与 `secret`。
2. 准备可拉的 HTTP-FLV（或 ZLM 已支持的其它拉流 URL）。
3. 配置：

```bash
cp config.example.yaml config.yaml
# 编辑 listen.publicHost、zlm.*、devices[]
```

4. 编译运行：

```bash
go build -o bin/relay ./cmd/relay
./bin/relay -config config.yaml
```

或：

```bash
go run ./cmd/relay -config config.yaml
```

SIGINT / SIGTERM 会注销设备并清理会话。

## ZLM 调用约定

点播成功时大致顺序：

1. `addStreamProxy`：`url=<flvUrl>` → `vhost/app/stream`
2. 根据 INVITE SDP：
   - UDP 或 TCP `setup:passive`（平台等设备连）→ `startSendRtp`
   - TCP `setup:active`（平台主动连设备）→ `startSendRtpPassive`
3. `BYE` / 失败 → `stopSendRtp` + `delStreamProxy`

`stream` 命名：`{channelId}_{callId前缀}`。

## 配置要点

见 `config.example.yaml`：

- `listen.publicHost`：写入 Contact / 应答 SDP `c=`，须上级可达
- `devices[].sip`：上级 `host/port/serverId/domain`、密码、`transport: udp|tcp`
- `devices[].channels[]`：`channelId` + `flvUrl` + `name`

## manscdp 说明

直接依赖 `github.com/mickeyzzc/gb28181-go/manscdp`（v0.15.1）。该子包自带 GB2312/GBK 声明处理与 Catalog/Keepalive 等类型；本仓库 **不 vendor、不复制** 其源码。

适配点：Catalog 的 `<Query>` 可走 `Decode`；**DeviceInfo / DeviceStatus 平台查询也是 `<Query>` 根**，但 manscdp 类型是 `<Response>` 根，`Decode` 会报错——用 `internal/catalog.ProbeMessage`（`manscdp.UTF8Body` + 最小头解析）取 SN/CmdType，再用 `Encode` 回包。若日后上游补齐 Query 类型，可删掉 probe。

## 已知缺口 / 下一步

- 未做 TCP 监听与 UDP 同端口冲突时的精细分流；多 transport 时会对同一 `listen` 地址分别 `ListenAndServe`
- Catalog 大目录分包（多 MESSAGE）未做
- 未应答 `SUBSCRIBE`（Catalog 订阅 / 移动位置等）
- INVITE 鉴权、SSRC 自生成策略、ZLM 拉流就绪等待（`getMediaList` 轮询）可加强
- 无自动化联调测试（需真实/模拟上级 + ZLM）

## License

内部使用；第三方依赖遵循各自许可证（sipgo、gb28181-go/manscdp、等）。
