Prototype implementation of the console daemon described at:

<https://github.com/CCI-MOC/hil/issues/417#issuecomment-303564763>

The details of the API differ slightly from what is describe there. This
document provides an up-to-date description of the server's use. The
intended audience is familiar with current HIL internals; we will likely
change the explanations to avoid this prerequisite in the future.

# Configuration

A Config file is needed, whose contents should look like:


```json

{
    "ListenAddr": ":8080",
    "AdminToken": "83hg98g3h32"
}
```

The Admin token should be (cryptographically) randomly generated.

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
    "addr": "10.0.0.4",
    "user": "ipmiuser",
    "pass": "ipmipass"
}
```

Notes:

* The `node_id` is an arbitrary label.
* The fields in the body of the request are passed directly to ipmitool

### Setting the owner of a node

`PUT /node/{node_id}/owner`

Request body:

```json
{
    "owner": "alice"
}
```

### Removing the owner of a node

`DELETE /node/{node_id}/owner`

### Getting a new console token

Request body:

`POST /node/{node_id}/console-endpoints`

```json
{
    "owner": "alice"
}
```

Response body:

```json
{
    "token": "6119cdf777334998d7068dece09069b8"
}
```

Notes:

* The owner in the request must match the current owner of the node.
* The token in the response is to be used to view the console.

## Non-admin operations


### Viewing the console

`GET /node/{node_id}/console?token=<token>`

Notes:

* The `<token>` is fetched as described above
* Data from the console will begin streaming from the response body.

# Extras

* If the `-dummydialer` cli option is passed, rather than launching
  ipmitool, the server will simply open a tcp connection to the
  "addr" specified (in which case it should be of the form required
  by [net.Dial][1]. This is useful for experimentation.
* There's some preliminary work on supporting a database, but it isn't
  actually used. The `-dbpath` argument sets the path, but the db won't
  be used beyond initializing a schema.

[1]: https://golang.org/pkg/net/#Dial
