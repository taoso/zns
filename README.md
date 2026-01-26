# ZNS - Simple DoH server in Go

## 安装

```bash
go install github.com/taoso/zns/cmd/zns@latest
```

## 运行

```bash
zns -token tokens.toml -tls-hosts zns.example.org -root /var/www/html
```

其中 tokens.toml 格式如下：

```toml
token1 = true  # 有效令牌
token2 = false # 无效令牌
# ....
```

对应的 DoH 链接为 `https://zns.example.org/dns/token1`。

## 原理

<https://taoshu.in/dns/diy-doh.html>

## 星标趋势

<a href="https://www.star-history.com/#taoso/zns&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=taoso/zns&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=taoso/zns&type=Date" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=taoso/zns&type=Date" />
 </picture>
</a>
