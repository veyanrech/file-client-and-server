# file-client-and-server

## Configuration

All runtime parameters are stored in JSON config files.

Server config: `server/config.json`

```json
{
  "addr": ":8881",
  "root": "./server_files",
  "token": "secret-token",
  "ignore_file": ".serverignore"
}
```

`ignore_file` is resolved relative to `root` when it is not absolute. If the
file does not exist, the server runs without ignore rules. Ignored files are not
included in `/manifest` and cannot be downloaded through `/download/{path}`.

Example `.serverignore`:

```gitignore
.git/
.env
node_modules/
*.log
tmp/
```

Client config: `client/config.json`

```json
{
  "server_url": "http://73.99.134.51:8881",
  "root": "./client_files",
  "token": "secret-token",
  "interval_seconds": 5
}
```

Run the server:

```bash
cd server
go run . -config config.json
```

Run the client:

```bash
cd client
go run . -config config.json
```
