# Deploying FRIDAY

One machine runs everything: Caddy for TLS, FRIDAY, and MySQL. Only Caddy is
reachable from outside.

```
        internet
           │  :443
           ▼
    ┌──────────────┐
    │    Caddy     │   owns the certificate, renews it indefinitely
    └──────┬───────┘
           │  HTTP, loopback only
           ▼
    127.0.0.1:8080  FRIDAY
           │
           ▼
    127.0.0.1:3306  MySQL
```

FRIDAY speaks plain HTTP and its tokens are bearer credentials, so anyone who
can read one becomes that client. It therefore binds the loopback and nothing
else, and refuses to start on a public interface when `env = production`.

## What you need

- A machine with a public address. Oracle Cloud's Always Free tier gives two
  ARM cores and 12 GB at no cost, which is far more than this needs.
- A hostname. Let's Encrypt will not certify a bare IP, so an address alone is
  not enough. [DuckDNS](https://duckdns.org) gives five free subdomains that
  never expire.
- Ports 22, 80 and 443 reachable. Nothing else.

## 1. The name

Register a subdomain at DuckDNS and point it at the machine:

```bash
curl "https://www.duckdns.org/update?domains=friday-yourname&token=YOUR_TOKEN&ip="
dig +short friday-yourname.duckdns.org    # should answer with the machine's address
```

Leaving `ip=` empty makes DuckDNS use the address the request came from, so
run it on the machine itself.

## 2. The machine

```bash
sudo apt update && sudo apt install -y mysql-server caddy

sudo useradd --system --home /opt/friday --shell /usr/sbin/nologin friday
sudo mkdir -p /opt/friday && sudo chown friday:friday /opt/friday
```

Oracle's images arrive with everything closed; open only what is served:

```bash
sudo iptables -I INPUT -p tcp --dport 80  -j ACCEPT
sudo iptables -I INPUT -p tcp --dport 443 -j ACCEPT
sudo netfilter-persistent save
```

The security list in Oracle's console has to allow 80 and 443 as well; the
host firewall alone is not enough.

## 3. The database

On a 1 GB machine — which both free tiers are — MySQL's defaults take most of
the memory and leave nothing for FRIDAY. Add swap first, then the tuning:

```bash
sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile
sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
echo 'vm.swappiness=10' | sudo tee /etc/sysctl.d/99-friday.conf
sudo sysctl -p /etc/sysctl.d/99-friday.conf
```

```bash
sudo cp mysql-1gb.cnf /etc/mysql/mysql.conf.d/friday.cnf
sudo systemctl restart mysql
```

That takes MySQL from roughly 400 MB to 200 MB. Swap is a safety net, not a
plan: with `vm.swappiness=10` the machine prefers RAM and uses swap only to
avoid the OOM killer choosing a victim for it.

**Add the tuning after MySQL has finished installing, never during.** Touching
the service while `apt` is still configuring the package interrupts the
initialisation of the data directory and leaves the package in `iF` state,
which then has to be repaired with `dpkg --configure -a`. Check `dpkg -l
mysql-server-8.0` shows `ii` before doing anything else to it.

```bash
sudo mysql
```

```sql
CREATE DATABASE friday CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'friday'@'127.0.0.1' IDENTIFIED BY 'a long random password';
GRANT ALL PRIVILEGES ON friday.* TO 'friday'@'127.0.0.1';
FLUSH PRIVILEGES;
```

MySQL binds the loopback by default on Debian and Ubuntu. Confirm it, because
a database open to the internet is a worse problem than anything else here:

```bash
ss -lntp | grep 3306        # must show 127.0.0.1:3306, never *:3306
```

## 4. FRIDAY

Build for the server's architecture and copy it across:

```bash
make deploy-build                                   # linux/arm64
scp build/friday deployments/backup.sh  user@host:/tmp/
scp config.example.ini                  user@host:/tmp/
```

On the machine:

```bash
sudo mv /tmp/friday /tmp/backup.sh /opt/friday/
sudo mv /tmp/config.example.ini /opt/friday/config.ini
sudo chown friday:friday /opt/friday/*
sudo chmod 750 /opt/friday/friday /opt/friday/backup.sh
sudo chmod 600 /opt/friday/config.ini      # it holds the database password
```

Edit `/opt/friday/config.ini`:

```ini
env = production

[server]
addr = 127.0.0.1:8080

[database]
password = the password set above

[provider]
name = platformai

[platformai]
client_id     = ...
client_secret = ...
refresh_token = ...
```

Secrets can go in the environment instead, through a systemd drop-in, if the
file being readable by root is not acceptable:

```bash
sudo systemctl edit friday
```
```ini
[Service]
Environment="FRIDAY_DATABASE_PASSWORD=..."
Environment="FRIDAY_PLATFORMAI_CLIENT_SECRET=..."
Environment="FRIDAY_PLATFORMAI_REFRESH_TOKEN=..."
```

Then install the service:

```bash
sudo cp friday.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now friday
journalctl -u friday -f
```

The schema is created on the first start; there is no separate migration step.

## 5. TLS

Put your hostname into the `Caddyfile`, then:

```bash
sudo cp Caddyfile /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Caddy obtains the certificate on the first request to that name. If it fails,
the cause is almost always DNS not yet pointing at the machine, or port 80
closed — the challenge needs both.

```bash
curl https://friday-yourname.duckdns.org/health
# {"status":"ok"}
```

## 6. An account

```bash
sudo -u friday /opt/friday/friday createuser yourname
```

Then log in from a client. The token comes back once:

```bash
curl -X POST https://friday-yourname.duckdns.org/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"yourname","password":"...","client_name":"my phone"}'
```

## 7. Backups

One machine means one backup problem, and everything FRIDAY knows lives in
MySQL.

```bash
sudo cp friday-backup.service friday-backup.timer /etc/systemd/system/
sudo mkdir -p /var/backups/friday && sudo chown friday:friday /var/backups/friday
sudo systemctl daemon-reload
sudo systemctl enable --now friday-backup.timer

sudo systemctl start friday-backup.service    # prove it works now
systemctl list-timers friday-backup.timer
```

The script keeps fourteen days, writes to a temporary name until the dump is
complete so a half-written file is never mistaken for a good one, and checks
afterwards that the archive is readable and contains the tables. A backup that
restores nothing is worse than none, because it is trusted.

**Copy them off the machine.** A backup on the disk that dies with it is not a
backup:

```bash
rsync -az --delete user@host:/var/backups/friday/ ~/friday-backups/
```

Restoring:

```bash
zcat friday-20260921T033000Z.sql.gz | mysql -u friday -p friday
```

## Checking it afterwards

```bash
systemctl status friday caddy mysql
curl -sI https://friday-yourname.duckdns.org/health | head -1
ss -lntp | grep -E ':(8080|3306)'      # both must be 127.0.0.1, never *
curl -s http://YOUR_PUBLIC_IP:8080/health    # must fail to connect
```

That last one matters most: if it answers, FRIDAY is reachable without TLS and
every token it has issued should be revoked.

## Upgrading

```bash
make deploy-build
scp build/friday user@host:/tmp/
ssh user@host 'sudo systemctl stop friday \
  && sudo mv /tmp/friday /opt/friday/friday \
  && sudo chown friday:friday /opt/friday/friday \
  && sudo systemctl start friday'
```

Stopping gives FRIDAY time to drain requests and let running tasks record
where they reached; any it cannot finish are failed on the next start rather
than left reading `running` for ever.

## When something is wrong

| What you see | Usually |
|---|---|
| Caddy will not get a certificate | DNS not pointing here yet, or port 80 closed. Both are needed. |
| `502` from Caddy | FRIDAY is not running. `journalctl -u friday -n 50` |
| Refuses to start, "public interface" | `addr` is not loopback and `env = production`. That is the check working. |
| Refuses to start, "password must be set" | `[database] password` is empty in production. |
| Streaming arrives all at once | Something between is buffering. `flush_interval -1` must be in the `Caddyfile`. |
| Every token rejected after a restore | Restored over a different database. Tokens are hashed per row; they do not move between databases. |
