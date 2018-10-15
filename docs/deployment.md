# Deploying OBMd

## Download OBMd

1. Download the OBMd binary from github. This downloads v0.2 of obmd.

`$ wget https://github.com/CCI-MOC/obmd/releases/download/v0.2/obmd`

Alternatively, you can clone the source code and build the binary yourself.

2. Make it executable and place it in `/usr/local/bin`.

```
$ sudo chmod +x obmd
$ sudo mv obmd /usr/local/bin/
```

3. If you decide to move obmd to a different directory then make sure to update
the obmd service file.

## The configuration file

1. Create a configuration file for obmd `/etc/obmd/config.json`.
See the [README](https://github.com/CCI-MOC/obmd/blob/master/README.md) for
complete documentation about the configuration file.

2. Create a system user for running OBMd and change the ownership of the configuration
file to that user.
```
$ useradd obmd-user -d /var/lib/obmd -m -r
$ chown obmd-user:obmd-user /etc/obmd/config.json
```

3. Generate an admin token by running `obmd -gen-token`.

4. You can specify `sslmode=disable` in the DBPath if your postgres server is
running without TLS. This is okay only if the postgres server and OBMd are on
the same host. However, if the postgres server is on a different system, it is
*important* to use TLS.

5. Specify the path to the TLS cerfiticate and Key in the configuration.
You can use [Let's Encrypt](https://letsencrypt.org/)  to generate free certificates.
[Certbot](https://certbot.eff.org/) makes this process easier.

Sample config file:
```
{
	"DBType":     	"postgres",
	"DBPath":     	"host=localhost port=5432 user=obmd-user password=password dbname=obmd sslmode=disable",
	"ListenAddr": 	"IPADDR:8080",
	"AdminToken": 	"12345678912345678912345678912345",
	"TLSCert":	"server.crt",
	"TLSKey":	"server.key"
}
```

## Running obmd as a service using systemd

1. Copy the systemd service file `scripts/obmd.service` to `/usr/lib/systemd/system/`
if you are running RHEL/fedora/CentOS. For ubuntu, the path
would be `/lib/systemd/system`.

2. Run these commands in sequence:

```
$ systemctl enable obmd.service
$ systemctl start obmd.service
```

3. Check the status to make sure it's running.
`$ systemctl status obmd.service -l`

## Running OBMd with Apache

OBMd is a go web app and is perfectly capable of being an internet-facing web server.
However, if you want to run multiple virtual servers on the same machine
like running HIL and OBMd together, you can use Apache as an HTTP proxy. This also means
you need to configure only your Apache server with TLS.

1. Make sure that your Apache server is running with TLS.

2. In the obmd config file at `/etc/obmd/config.json`, enable obmd to run on
localhost on some port, with Insecure option set to true.

Sample config file:
```
{
	"DBType":     	"postgres",
	"DBPath":     	"host=localhost port=5432 user=obmd-user password=password dbname=obmd sslmode=disable",
	"ListenAddr": 	"127.0.0.1:8080",
	"AdminToken": 	"12345678912345678912345678912345",
	"Insecure":	true
}
```

3. Create a configuration file `/etc/httpd/conf.d/obmd.conf` that tells apache
how to act as a reverse proxy for obmd.

```
ProxyPass "/obmd" "http://localhost:8080"
ProxyPassReverse "/obmd" "http://localhost:8080"
```

This forwards any traffic coming to `https://example.com/obmd` to obmd.
Everything else is handled by the default server (which would be hil).

4. Restart Apache `systemctl httpd restart`.

5. When you register nodes in HIL, the OBMD URI will look like
`https://example.com/obmd/node/<node-name>`.
