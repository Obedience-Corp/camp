# Camp document API

Contract for WI-f46318 / CM0002. Schema version: `settings-documents/v1alpha1`.
This extends existing settings commands without changing their payloads.

## Commands

All commands accept `--campaign <exact-registered-id-or-name> --json` and resolve
on the executing machine. Paths are relative to the selected `.campaign/`.
Known catalog IDs are also accepted wherever a path is accepted.

```text
camp settings catalog
camp settings document get <path>
camp settings document validate <path> --stdin
camp settings document set <path> --expected-revision <revision> --stdin
camp settings document create <path> --expected-revision absent --stdin
camp settings document patch <path> --expected-revision <revision> --stdin
camp settings document rename <path> <destination> --expected-revision <revision>
camp settings document delete <path> --expected-revision <revision>
camp settings document action <path> <action> --expected-revision <revision> --stdin
```

`validate`, `set`, and `create` read raw UTF-8 source from stdin. `patch` reads a
JSON object with `changes`, an array of `{op:"set"|"remove",path:string[],value?:unknown}`.
`set` requires `value` (including explicit null); `remove` has no value. Paths
address nested object keys; replace an array as a value to preserve its order.
The service applies patches to the existing parsed document while preserving
unknown keys and YAML comments. A patch never executes commands in configuration.

Mutation responses are persisted snapshots. Rename returns the destination
snapshot and refuses an existing destination. Delete returns an absent snapshot
for the original path. Revisions are `absent` or `sha256:<lowercase-hex>`.
Revision comparison and absence checks occur under the owner lock.

Create makes missing parent directories only after validating the candidate and
its capabilities. A failed create removes only empty directories created by
that operation. Symlink parents are refused; creation depth is bounded to 16
path components. Rename requires an existing destination directory and never
overwrites a destination. If publishing succeeds but a later durability or
readback step fails, the response reports `applied:true`.
Post-publication filesystem failures use `io_error` with `applied:true` and
describe the persisted or partial state. `readback_failed` is reserved for a
failed readback after the mutation itself completed.

Owner actions accept JSON bodies and return a persisted snapshot with
`applied:true`. `settings/pins.json` supports `pin` with `{name:string,path:string}`
and `unpin` with `{name:string}`. `settings/workitems.json` supports
`workitem-priority` with `{selector:string,priority:"high"|"medium"|"low"|"clear"}`
and `workitem-group` with `{selector:string,group:string}` (empty group clears).
Workitem selectors use the owner's stable ID/key/path resolver. Unsupported
actions are errors; capabilities distinguish these executable actions from
commands offered for navigation to another owner.

## JSON types

```typescript
type Diagnostic = {
  severity: "error" | "warning";
  message: string;
  path?: string;
  line?: number;
  column?: number;
};
type OwnerAction = {
  id: string;
  label: string;
  command: string[];
  supported: boolean;
  reason?: string;
};
type Field = {
  path: string[];
  label: string;
  type: "string" | "boolean" | "number" | "object" | "array";
  description?: string;
  enum?: unknown[];
  default?: unknown;
  read_only?: boolean;
  properties?: Field[];
  items?: Field;
  additional_properties?: Field;
};
type DocumentEntry = {
  id: string;
  path: string;
  title: string;
  scope: "camp";
  format: "yaml" | "json" | "markdown" | "text" | "binary" | "directory" | "symlink";
  owner: string;
  exists: boolean;
  capabilities: {
    read: boolean;
    source_edit: boolean;
    create: boolean;
    rename: boolean;
    delete: boolean;
    form: boolean;
    reason?: string;
    actions: OwnerAction[];
  };
  fields?: Field[];
  link_target?: string;
};
type Identity = {
  schema_version: "settings-documents/v1alpha1";
  camp_id: string;
  camp_root: string;
};
type Catalog = Identity & {
  documents: DocumentEntry[];
  limits: { max_depth: number; max_entries: number; max_document_bytes: number };
  truncated: boolean;
  diagnostics: Diagnostic[];
};
type Snapshot = Identity & DocumentEntry & {
  revision: string;
  content: string | null;
  values?: unknown;
  effective_values?: unknown;
  inheritance?: unknown;
  diagnostics: Diagnostic[];
  applied: boolean;
};
type Validation = Identity & {
  path: string;
  valid: boolean;
  diagnostics: Diagnostic[];
};
type ErrorResponse = {
  schema_version: "settings-documents/v1alpha1";
  error: { code: string; message: string; diagnostics?: Diagnostic[]; applied?: boolean; path?: string };
  camp_id?: string;
  camp_root?: string;
};
```

Successful JSON is written to stdout. Failed operations exit nonzero and write
the JSON error to stderr. Revision conflicts use `revision_conflict`.
Validation returns `valid:false` with diagnostics for an invalid candidate;
invalid paths and transport failures are errors. Clients reject unsupported
schema versions and tolerate additive fields.

If persistence succeeds but immediate readback fails, return `readback_failed`
with `error.applied:true` and the original target identity/path. Its message says
the write succeeded and readback must be retried. Snapshot `applied` is false
for reads and true for mutation readback. Never label an applied write a rejected
revision conflict.

## Ownership and bounds

Catalog discovery includes authored extensions and managed files, directories,
and symlinks without following symlinks. Machine-wide files are outside this
API. Symlinks expose their target and a capability reason; they do not grant
access outside `.campaign/`. Every noneditable document explains the restriction
and advertises known owner actions. Watcher registration alone does not imply
that a document is generated or uneditable.

Known schemas derive from Camp config types. Identity and owner-managed lifecycle
fields retain their owner's mutation rules. Source writes preserve the supplied
bytes after validation; forms use `patch` so unrelated comments and keys survive.
Reads and validation never scaffold missing files or rewrite configuration.

Fixed struct members use `properties`; arrays use `items`; maps with dynamic
keys use `additional_properties` for the value schema. Paths on child members
are relative to their object. Item and map-value descriptor paths are empty.
For example, `shortcuts` is an object with `additional_properties` describing
one shortcut, so forms can add a shortcut name and edit its nested fields.
