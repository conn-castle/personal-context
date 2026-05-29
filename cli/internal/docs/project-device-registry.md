# Project and device registry

Records and chats are attributed to a **device** (where the data came from) and,
optionally, a **project**.

## Devices

- Register a device with `pc device register <id>`; list with
  `pc device list`.
- `pc chat import --device <id>` is required and the device must be registered
  and not archived. Provenance is always explicit — the importer never guesses a
  device.

## Projects

- Register a project with `pc project register <id>` (slash convention for
  hierarchy, e.g. `org/project`). Register the on-disk paths a project occupies
  on a device with `pc project register <id> <path> --device <device-id>`.
- Project assignment is nullable: a chat session keeps `project_id = NULL` until
  a registered path matches its working directory.

## Path backfill

When a chat session has a `cwd`, import assigns its project by the longest
registered project path on the same device whose path is a prefix of the `cwd`.
Sessions without a matching registered path remain unassigned. Re-running import
or `pc chat list --unassigned` helps surface sessions that still need a project
path registered.
