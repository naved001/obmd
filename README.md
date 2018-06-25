[![Build Status][travis-img]][travis]

Prototype implementation of the console daemon described at:

<https://github.com/CCI-MOC/hil/issues/417#issuecomment-303564763>

The details of the API differ slightly from what is describe there. This
document provides an up-to-date description of the server's use. The
intended audience is familiar with current HIL internals; we will likely
change the explanations to avoid this prerequisite in the future.

# Configuration

A config file is needed, whose contents should look like:


```json
{
	"DBType":     	"sqlite3",
	"DBPath":     	"./obmd.db",
	"ListenAddr": 	":8080",
	"AdminToken": 	"44d5ebcb1aae23bfefc8dca8314797eb",
	"TLSCert":	"server.crt",
	"TLSKey":	"server.key"
}
```

The choices for database type are `sqlite3` and `postgres`.
If using postgres, the DBPath string might look like:

	"host=localhost port=5432 user=username password=pass dbname=obmd"

The admin token should be a (cryptographically randomly generated)
128-bit value encoded in hexadecimal. You can generate such a token by
running:

    ./console-service -gen-token

By default, OBMd listens for connections via https. While production
environments should *never* change this, it can be convenient for
development to make OBMd listen via plaintext http. To do this, add an
option `"Insecure": true` in the config file, and remove the `TLSCert`
and `TLSKey` options.

By default, the server looks for the config file at `./config.json`, but
the `-config` command line option can be used to override this.

# Api

The server provides a simple REST api. Most operations are "admin"
operations. This file describes the api in a similar format to that used
by `docs/rest_api.md` in the HIL source tree.

## Admin Operations

Each admin operation requires the client to authenticate using basic
auth, with a username of "admin" and a password equal to the
"AdminToken" in the config file.

### Registering a node

`PUT /node/{node_id}`

Request body:

```json
{
    "type": "ipmi",
    "info": {
        "addr": "10.0.0.4",
        "user": "ipmiuser",
        "pass": "ipmipass"
    }
}
```

Notes:

* The above is for ipmi controllers; right now this is the only
  "real" driver, but there are other possible values of `"type"`
  that are used for testing/development. For those, see the
  relevant source under `./internal/driver`.
* The `node_id` is an arbitrary label.
* The fields in the `info` field are passed directly to ipmitool
* If the node already exists, this will return an error. To change
  the info for a node, you must delete it and re-register it.

### Unregistering a node

`DELETE /node/{node_id}`.

Notes:

* This implicitly invalidates any active tokens.

### Getting a new console token

Request body:

`POST /node/{node_id}/token`

Response body:

```json
{
    "token": "6119cdf777334998d7068dece09069b8"
}
```

Notes:

* The token in a successful response is to be used to authenticate
  non-admin operations, described below.

### Invalidating a console token

`DELETE /node/{node_id}/token`

Notes:

* This operation always returns a successful error code (assuming the
  user authenticates correctly); if there is no existing valid token for
  the node, this is a no-op.

## Non-admin operations

Each non-admin operation requires a `token` parameter in the query
string, like:

`GET /url/for/operation?token={token}`

Where `{token}` is fetched as described above. This parameter is not
explicitly mentioned in each of the descriptions below.

### Viewing the console

`GET /node/{node_id}/console`

Notes:

* Data from the console will begin streaming from the response body, and
  continue doing so until the connection is closed.

### Rebooting a node

`POST /node/{node_id}/power_cycle`

Request body:

```json
{
    "force": true
}
```

Power cycle the given node.

Notes:

* If `"force"` is set to `true`, The node will be forced off. Otherwise,
  the node will be sent an ACPI shutdown request, which the operating
  system may respond to.
* If the node is powered off, this will turn it on.

### Powering on a node

`POST /node/{node_id}/power_on`

Notes:

* Powers on the node. If the node is already powered on, this will
  have no effect.

### Powering off a node

`POST /node/{node_id}/power_off`

Notes:

* Powers off the node. If the node is already powered off, this will
  have no effect.

### Setting the boot device

`PUT /node/{node_id}/boot_device`

Request body:

```json
{
    "bootdev": "disk"
}
```

Notes:

* This sets the node's boot device persistently.
* The set of legal values for `"bootdev"` depends on the type of OBM.
* For IPMI devices, the legal values are:
  * `"pxe"`: Do a PXE (network) boot.
  * `"disk"`: Boot from local hard disk.
  * `"none"`: Reset boot order to default.

### Checking a node's power status

`GET /node/{node_id}/power_status`

Response body:

```json
{
    "power_status": "<IPMI chassis power status response>"
}
```

Notes:

* Returns a JSON string that describes the node's power state.
* Response examples: "on" or "off"

[net.Dial]: https://golang.org/pkg/net/#Dial
[travis]: https://travis-ci.org/CCI-MOC/obmd
[travis-img]: https://travis-ci.org/CCI-MOC/obmd.svg?branch=master
