Setup on Debian with systemd and nginx, roughly:

```
cd server && go build -trimpath -ldflags="-s -w" -o nodebench-server .
install -m 755 nodebench-server /usr/local/bin/
install -Dm 644 ../nodebench.sh /usr/local/share/nodebench/nodebench.sh
install -m 644 ../deploy/nodebench.service /etc/systemd/system/
systemctl daemon-reload && systemctl enable --now nodebench

install -m 644 ../deploy/nginx.conf /etc/nginx/sites-available/nodebench
ln -s /etc/nginx/sites-available/nodebench /etc/nginx/sites-enabled/
nginx -t && systemctl reload nginx
```

Change the hostname in both files if you run it somewhere else.
