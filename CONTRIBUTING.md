# Contributing

Build, test and project layout are in the README's [Development](README.md#development) section.

## Testing a branch

`scripts/local.sh` builds the image from whatever is checked out and runs it in Docker with the
same restrictions as a real install (read-only, non-root, no capabilities), at
`https://localhost:8443`. Files live in `~/strata-local`, so your passphrase and test files carry
over between branches.

```bash
git switch some-branch
./scripts/local.sh          # build and (re)start; prints the setup code on first run
./scripts/local.sh logs     # follow the logs
./scripts/local.sh down     # stop it
./scripts/local.sh reset    # delete ~/strata-local and start fresh
```

The startup log line names the branch and commit it was built from. `STRATA_LOCAL_PORT` and
`STRATA_LOCAL_DIR` change the port and the folder.
