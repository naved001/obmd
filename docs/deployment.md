# Deploying OBMd

## Download OBMd

1. Download the OBMd binary from github. This downloads v0.2 of obmd.

`$ wget https://github.com/CCI-MOC/obmd/releases/download/v0.2/obmd`

Alternatively, you can clone the source code and build the binary yourself.

2. Make it executable and place it in `/bin`.

```
$ sudo chmod +x obmd
$ sudo mv obmd /bin/
```

3. If you decide to move obmd to a different directory then make sure to update
the obmd service file.

## The configuration file

1. Create a configuration file for obmd `/etc/obmd/config.json`.

2. Generate an admin token by running `obmd -gen-token`.

3. You can specify `sslmode=disable` in the DBPath if your postgres server is
running without TLS. This is okay only if the postgres server and OBMd are on
the same host. However, if the postgres server is on a different system, it is
*important* to use TLS.

4. Specify the path to the TLS cerfiticate and Key in the configuration.
You can use [Let's Encrypt](https://letsencrypt.org/)  to generate free certificates.
[Certbot](https://certbot.eff.org/) makes this process easier.

## Running obmd as a service using systemd

1. Copy the systemd service file `scripts/obmd.service` to `/usr/lib/systemd/system/`
if you are running RHEL/fedora/CentOS. For ubuntu, the path
would be `/lib/systemd/system`.

Note: The service file runs the obmd service as the hil user.

2. Run these commands in sequence:

```
$ systemctl enable obmd.service
$ systemctl start obmd.service
```

3. Check the status to make sure it's running.
`$ systemctl status obmd.service -l`

