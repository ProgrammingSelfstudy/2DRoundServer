# 部署到云服务器（先用 IP，没有域名）

一台 Linux 服务器跑全部东西：三个 Go 服务、H5 页面和资源。玩家打开 `http://服务器IP:8767`。

| 端口 | 谁在听 | 对外 | 用途 |
|---|---|---|---|
| 8767 | Caddy | 开放 | H5 页面、`/cdn` 资源、`/loginapi` 登录服务 |
| 8802 | gateway | 开放 | 游戏连接（WebSocket） |
| 8081 / 8800 / 8801 / 8080 | 三个 Go 服务 | 不开放 | 只在本机；后台用 SSH 隧道访问 |

没有域名就没有证书，所以全程是 `http` / `ws`，**不加密**：登录密码在网络上是明文。只给自己和少数人用；
管理后台因此不对外，只走 SSH 隧道。

服务器选香港或海外机房更省事。安全组 / 防火墙只放行 22、8767、8802。

## 1. 本机打包

```sh
sh deploy/build.sh            # x86 服务器；ARM 服务器用 sh deploy/build.sh arm64
```

得到 `deploy/2dround-server-linux-amd64.tar.gz`（三个程序、配置样例、Caddyfile、systemd 单元）。

## 2. 服务器上安装（root）

```sh
# 装 Caddy：https://caddyserver.com/docs/install （Debian/Ubuntu 有 apt 源）
useradd --system --home /opt/2dround --shell /usr/sbin/nologin 2dround
mkdir -p /opt/2dround/{data,h5,cdn}
tar -xzf 2dround-server-linux-amd64.tar.gz -C /opt/2dround

cd /opt/2dround
cp configs/prod.example.yaml configs/prod.yaml
sed -i "s/YOUR-SERVER-IP/你的服务器IP/g" configs/prod.yaml
sed -i "s/jwt_secret: CHANGE-ME.*/jwt_secret: $(openssl rand -hex 32)/" configs/prod.yaml
chmod 600 configs/prod.yaml
chown -R 2dround: /opt/2dround

cp 2dround-*.service /etc/systemd/system/
cp Caddyfile /etc/caddy/Caddyfile
systemctl daemon-reload
systemctl enable --now 2dround-loginhttp 2dround-gateway 2dround-admin
systemctl reload caddy
```

管理员的初始密码只在 admin 第一次启动时打印一次：

```sh
journalctl -u 2dround-admin | grep -i password
```

## 3. 传 H5 页面和资源（本机）

```sh
rsync -az --delete ~/Documents/code/2DRoundClient/mini/build/web-mobile/ root@服务器IP:/opt/2dround/h5/
rsync -az          ~/Documents/code/2DRoundAssets/build/dev54/           root@服务器IP:/opt/2dround/cdn/
```

资源约 400 MB、7000 多个文件，第一次要传一会儿；以后只传变化的部分。`/opt/2dround/h5` 和 `cdn` 要让 Caddy 读得到
（`chmod -R a+rX /opt/2dround/h5 /opt/2dround/cdn`）。

## 4. 在后台发布资源版本

```sh
ssh -L 8080:127.0.0.1:8080 root@服务器IP      # 保持这个窗口开着
```

浏览器打开 `http://127.0.0.1:8080`，登录后进「客户端资源」，平台选「网页 H5」：

- 资源 CDN 地址：`http://服务器IP:8767/cdn`
- 网关 WebSocket 地址：留空（页面会自己连 `ws://服务器IP:8802/ws`）
- 新建版本 `dev54`，清单地址 `http://服务器IP:8767/cdn/manifest.web.json`，然后发布

## 5. 检查

```sh
curl "http://服务器IP:8767/loginapi/client/config?platform=h5&ver=0.1"   # 有 cdn_base 和已发布的 release
curl -I "http://服务器IP:8767/cdn/manifest.web.json"                     # 200
```

手机用移动数据打开 `http://服务器IP:8767`，注册登录，进主城。测试的人都有账号后，把 `configs/prod.yaml` 里的
`auto_register` 改成 `false` 并 `systemctl restart 2dround-loginhttp`，没有账号的人就进不来了。

## 更新

```sh
sh deploy/build.sh && scp deploy/2dround-server-linux-amd64.tar.gz root@服务器IP:/tmp/
# 服务器上：
tar -xzf /tmp/2dround-server-linux-amd64.tar.gz -C /opt/2dround ./bin && systemctl restart 2dround-loginhttp 2dround-gateway 2dround-admin
```

重启网关会断开在线玩家，页面要重新登录。数据库在 `/opt/2dround/data`，admin 每天自动备份到 `data/backups`。

## 以后有了域名

1. 域名解析到服务器，放行 80 / 443。
2. `Caddyfile` 里把 `:8767` 换成域名，并按文件开头的注释加上 `/ws` 转发；Caddy 会自动申请证书。
3. `prod.yaml`：`ws_addr` 改回 `127.0.0.1:8802`，`ws_origins` 改成域名，`max_conns_per_ip` 改成 `0`
   （连接都从 Caddy 进来，按 IP 限制会把所有人算成一个）。
4. 后台里把 CDN 地址、清单地址换成 `https://域名/cdn...`，网关 WebSocket 地址填 `wss://域名/ws`。
5. 到这一步页面是 https 了，H5 页面和后台页面就可以搬到 Vercel。
