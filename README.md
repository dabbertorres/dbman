# dbman

## Config

The default config file is located at `$HOME/.config/dbman/config.json`. Otherwise,
you can pass the `-cfg <my config file>` flag.

If the default config file doesn't exist, one is generated containing a connection
for connecting to a postgresql database running on your localhost (or a container, etc).

Example configuration:

```json
{
  "connections": {
    "example": {
      "host": "the hostname or IP address running the database",
      "port": 5432,
      "database": "database name to connect to on the instance",
      "username": "username",
      "password": "optional - if required, you'll be prompted for it when connecting",
      "driver": "postgres (only driver tested at this time)",
      "driver_opts": {
        "set of": "driver specific settings",
        "for connecitng": "for example",
        "sslmode": "verify-full"
      },
      "tunnel": "the name of a tunnel configuration (optional)",
      "connect_timeout_sec": 30,
      "max_open_conns": 4
    }
  },
  "tunnels": {
    "example": {
      "host": "the hostname or IP address of the tunnel server",
      "port": 22,
      "user": "ssh username",
      "auth_method": "password OR public_key OR agent",
      "password": "only used if auth_method == password (optional, prompted for if needed)",
      "private_key_file": "only used if auth_method == public_key",
      "private_key_passphrase": "only used if auth_method == public_key, and private key is encrypted (optional, prompted for if needed)",
      "connect_timeout_sec": 30,
      "disable_verify_known_host": false,
      "host_public_key_file": "public key of the server, if it's not in your known hosts or otherwise in your SSH agent"
    }
  }
}
```

## Usage

### CLI

Run `dbman <connection name>` to connect to the named connection configuration.
If you forget what connections you have in your config file, run `dbman -list`.

### neovim plugin

Not 100% sure on a required version, but v0.4.4 (the latest stable, at the time
of writing) works nicely.
Fill out the default config file, copy `<repo root>/cmd/dbman-nvim/dbman.vim`
to your `~/.config/nvim/plugin/` directory, and make sure `dbman-nvim` is on
your `$PATH`.

Launch neovim! Here are the list of commands:

- `DBConnections`
  - lists available connections
- `DBConnect <connection name>`
  - connect to a database (has autocompletes support)
  - Unless disabled with `let g:db_auto_display_schema = 0`, a window should open
    displaying the accessible schemas and tables.
- `DBRefresh`
  - if you have the auto schema display disabled, this command will show it.
- `DBSchemas`
  - lists accessible schemas on the current connection.
- `DBTables`
  - lists accessible tables.
  - If no arguments are given, tables are listed in the public schema are listed.
  - If one or more arguments are given, tables in each schema are listed.
- `DBDescribe <table name>`
  - print a description of the named table's schema.
  - Use `schema_name.table_name` syntax for non\*public tables.
- `DBRun <optional buffer number>`
  - Executes SQL in your current buffer.
  - If you only want to run part of the SQL in your buffer, select what you want
    in visual mode!
  - If a buffer number was provided, the results of the query (if any) will be put
    in that buffer. Otherwise, a new buffer and window will be created to display
    the results.

## Status

This started as nothing but a prototype on the side, that evolved into what it is
now. Needless to say, the code needs a good amount of cleanup.
Otherwise, I've actually used it to connect to more than local scratch databases,
and it's worked fine for my needs.
