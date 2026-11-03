# gb28181-relay

纯 Go 国标 **假设备 / FLV→GB28181** 中继：多设备 REGISTER 到（可不同的）上级平台，通道源为 HTTP-FLV，媒体由 **ZLMediaKit HTTP API** 拉流并 `startSendRtp` / `startSendRtpPassive` 推 PS-RTP。本进程 **不自己推 RTP/PS**。

> 本机命令行运行；无网页 UI、无云端编排依赖。

## 首版能力

- 多设备 `REGISTER` + Digest
- 心跳 `Keepalive`（MESSAGE Notify）
- `Catalog` / `DeviceInfo` / `DeviceStatus`：查询 MESSAGE 先空 200，再主动 MESSAGE 上报；Catalog 按 UDP MTU 多包分页（同 SN / SumNum，DeviceList 分片）
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
- 出站 Via 带 `;rport`（NAT）；Contact/SDP 仍用 `listen.publicHost`
- `devices[].sip`：上级 `host/port/serverId/domain`、密码、`transport: udp|tcp`
- **REGISTER Request-URI** = `sip:{serverId}@{domain}`（LiveGBS / 常见国标上级）；**From/To** = `sip:{deviceId}@{domain}`。勿把设备 ID 放进 Request-URI，否则易被回 **400 Bad Request**
- `registerId` 已忽略（保留字段仅为兼容旧 YAML）
- `devices[].channels[]`：`channelId` + `flvUrl` + `name`

## 联调注意（REGISTER 400）

1. 确认上级登记的设备编码 / 域 / 平台编码与 `deviceId`、`domain`、`serverId` 一致；密码勿猜，以平台侧为准。
2. 启动后日志会打完整出站报文（`REGISTER outbound`）。**Via / CSeq / Max-Forwards** 由 sipgo `ClientRequestBuild` 在发送前补齐；若你在代码里打印的是 build 前的 Request，会看起来像“缺 Via/CSeq”，那是截断观感，不是真缺。
3. 不要用 sipgo 的 `ClientRequestRegisterBuild`：它按 RFC 3261 剥掉 Request-URI 的 userinfo，国标上级通常仍要 `serverId@domain`。
4. 401/407 后走 Digest（`username` 默认 `deviceId`）；先 400 说明报文格式/URI 被拒，还没到鉴权。
5. `publicHost` 须上级能回连（Contact）；仅改 URI 不够时再查网络/端口映射。

## 联调注意（Catalog / MESSAGE MTU）

sipgo UDP 发送上限约 **1300 字节**（`UDPMTUSize-200`）。把整份 Catalog XML 塞进查询的 **200 body** 会触发 `size of packet larger than MTU`。

当前行为：查询 MESSAGE → **空 200 OK** → 设备侧再发 **Request MESSAGE** 上报 Response；通道多时按 body≤800B 分页，每页同 `SN`/`SumNum`，`DeviceList Num` 为本页条数。日志应见 `Catalog MESSAGE sent page=…`，不应再出现 `MESSAGE 200 failed`。

## manscdp 说明

直接依赖 `github.com/mickeyzzc/gb28181-go/manscdp`（v0.15.1）。该子包自带 GB2312/GBK 声明处理与 Catalog/Keepalive 等类型；本仓库 **不 vendor、不复制** 其源码。

适配点：Catalog 的 `<Query>` 可走 `Decode`；**DeviceInfo / DeviceStatus 平台查询也是 `<Query>` 根**，但 manscdp 类型是 `<Response>` 根，`Decode` 会报错——用 `internal/catalog.ProbeMessage`（`manscdp.UTF8Body` + 最小头解析）取 SN/CmdType，再用 `Encode` 回包。若日后上游补齐 Query 类型，可删掉 probe。

## 已知缺口 / 下一步

- 未做 TCP 监听与 UDP 同端口冲突时的精细分流；多 transport 时会对同一 `listen` 地址分别 `ListenAndServe`
- 未应答 `SUBSCRIBE`（Catalog 订阅 / 移动位置等）
- INVITE 鉴权、SSRC 自生成策略、ZLM 拉流就绪等待（`getMediaList` 轮询）可加强
- 无自动化联调测试（需真实/模拟上级 + ZLM）

## License

内部使用；第三方依赖遵循各自许可证（sipgo、gb28181-go/manscdp、等）。
