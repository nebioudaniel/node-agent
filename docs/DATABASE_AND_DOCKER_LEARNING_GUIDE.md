# Team Compute Platform --- Database & Docker Learning Guide

> **Purpose:** Learn PostgreSQL, SQL, Docker, database design, Go
> database access, migrations, and integration testing by building the
> next durable-storage layer of Team Compute Platform.
>
> **Working style:** You write the code. This guide explains the
> concepts, gives small examples, assigns one task at a time, and tells
> you how to test your work. Do not copy a complete implementation
> blindly.

------------------------------------------------------------------------

## 0. How to use this guide

Work through the lessons in order. For each lesson:

1.  **Understand** the concept and why the project needs it.
2.  **Inspect** the current code and repository structure.
3.  **Design** the smallest change before coding.
4.  **Implement** the assigned task yourself.
5.  **Test** using the exact commands in the lesson.
6.  **Review** the diff and explain the design in your own words.
7.  **Commit** only after the checks pass.

Do not move to the next task just because the code compiles. You should
be able to explain what you built.

### Your learning loop

For every feature, answer these questions in your notes:

-   What problem does this solve?
-   What data enters this component?
-   What does it return or change?
-   What can fail?
-   Who owns this responsibility?
-   How do we test it?
-   Why did we choose this design instead of a simpler alternative?

### How to ask for tutoring help

When you get stuck, send only:

``` text
Lesson/task:
What I understand:
What I tried:
Command I ran:
Full error/output:
Relevant function or file:
```

Do not paste the entire repository unless requested. Ask for an
explanation or a hint first. The tutor should not immediately replace
your implementation with a finished solution.

------------------------------------------------------------------------

# Part I --- Understand the system before adding a database

## 1. What we are building

Team Compute Platform connects computers into a shared pool. Each worker
machine runs a small Go agent. A coordinator receives heartbeats, tracks
workers, and eventually assigns AI tasks to suitable workers.

The first version has three important parts:

-   **Agent:** runs on a worker computer, collects machine information,
    and sends heartbeats.
-   **Coordinator:** receives requests, authenticates workers, tracks
    worker state, and later schedules work.
-   **PostgreSQL:** stores durable information so important records
    survive a coordinator restart.

The coordinator should remain the source of truth for worker identity
and status. The agent reports facts; the coordinator decides what those
facts mean.

### The memory-versus-database distinction

The in-memory registry is useful because it makes frequent reads and
status calculations fast. But memory disappears when the coordinator
process stops.

PostgreSQL provides durable storage. It should not replace every
in-memory operation. The intended design is:

``` text
Agent heartbeat
      |
      v
Coordinator HTTP handler
      |
      v
In-memory registry  <---- fast reads for API and scheduler
      |
      | periodically persist changed durable fields
      v
Store interface
      |
      v
PostgreSQL
```

The registry should not know SQL or PostgreSQL details. It should depend
on a small storage interface. A PostgreSQL implementation satisfies that
interface.

### Why not write every heartbeat to PostgreSQL?

The build guide uses a heartbeat interval of about 5 seconds. If many
machines send heartbeats, writing every one directly to the database
creates unnecessary database traffic.

Instead:

-   Update the in-memory worker state on each accepted heartbeat.
-   Mark the worker's durable state as dirty when needed.
-   Flush changed `LastSeen` values in batches about every 15 seconds.
-   Flush once more during graceful shutdown.
-   Persist immediately when a durable event requires it, such as
    registration or a meaningful status change.

This is called **write-behind** or **write-behind batching**. It trades
a small amount of possible heartbeat freshness after a crash for fewer
database writes. It must be implemented carefully so important records
are not lost.

### Worker status recovery

When the coordinator starts:

1.  Load the workers from PostgreSQL.
2.  Restore their records into the in-memory registry.
3.  Do not assume they are online just because they were online before
    shutdown.
4.  Their old `LastSeen` timestamps determine their current status.
5.  A new heartbeat makes a reachable worker online again.

This avoids falsely reporting a disconnected machine as online after a
restart.

------------------------------------------------------------------------

# Part II --- Git workflow for every lesson

## 2. One issue, one feature branch, small commits

Use Git to preserve a clear engineering history. A branch should
represent one coherent feature or task, not every tiny line of code.

Recommended workflow:

``` text
Create issue
   |
   v
Create feature branch
   |
   v
Understand + implement one small task
   |
   v
Format + test + inspect diff
   |
   v
Commit
   |
   v
Push branch
   |
   v
Open PR / review / merge
```

### Before starting work

Inspect the repository:

``` bash
git status --short
git branch --show-current
git log -5 --oneline
```

Your current working tree has previously contained an untracked
`.DS_Store` and a `docs/` directory. Inspect them before switching
branches. Do not delete or overwrite `docs/` blindly.

Add this to `.gitignore` if it is not already present:

``` gitignore
.DS_Store
.env
.env.*
!.env.example
```

Never commit real passwords, tokens, API keys, or database connection
strings. An `.env.example` may contain placeholder values only.

### Branch naming

Examples:

``` text
feature/12-postgres-persistence
feature/13-database-migrations
feature/14-docker-postgres
feature/15-store-integration-tests
```

Use your actual issue number if you create GitHub issues. If you are not
using issues, omit the number.

### Commit messages

Use messages that explain the change:

``` text
docs: add database learning guide
test(store): add worker persistence tests
feat(store): implement postgres worker store
feat(db): add initial schema migration
chore(dev): add local postgres compose service
```

Do not make a commit if formatting, tests, or review checks fail.

### Contribution graph reminder

A local commit alone does not appear on GitHub. Push your commits to
GitHub, and make sure the commit email is associated with your GitHub
account. A private repository may also require the relevant
contribution-display setting.

------------------------------------------------------------------------

# Part III --- Database fundamentals

## 3. What is a database?

A database is an organized place to store and retrieve data. A
relational database stores data in tables and connects related records
using keys.

Think of a table as a structured collection of rows:

  id       hostname    os         ram_total_bytes
  -------- ----------- -------- -----------------
  wrk_a1   laptop-a    darwin         17179869184
  wrk_b2   desktop-b   linux          34359738368

-   A **row** represents one worker.
-   A **column** represents one property of a worker.
-   A **primary key** uniquely identifies a row.
-   A **foreign key** links a row to another table.
-   A **constraint** prevents invalid data from being stored.
-   An **index** helps the database find rows faster, at the cost of
    extra storage and write work.

### Primary key

A primary key must uniquely identify a record. In this project, worker
IDs are text identifiers such as `wrk_8f3a2c`.

``` sql
CREATE TABLE example_workers (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL
);
```

`NOT NULL` means the field is required. `PRIMARY KEY` means the value
must be unique and cannot be null.

### Foreign key

A task belongs to a worker when it is assigned:

``` sql
CREATE TABLE example_tasks (
    id TEXT PRIMARY KEY,
    worker_id TEXT REFERENCES example_workers(id)
);
```

The foreign key prevents a task from referencing a worker ID that does
not exist.

### Relationships

Common relational patterns:

-   **One-to-one:** one record relates to at most one record in another
    table.
-   **One-to-many:** one worker can be associated with many tasks over
    time.
-   **Many-to-many:** many users can belong to many groups; this usually
    needs a linking table.

### Data types you will encounter

  PostgreSQL type     Meaning                    Project example
  ------------------- -------------------------- ----------------------------
  `TEXT`              Variable-length text       worker ID, hostname
  `INT` / `INTEGER`   32-bit integer             CPU cores, token use count
  `BIGINT`            64-bit integer             RAM byte counts
  `BOOLEAN`           true/false                 feature flags
  `BYTEA`             Binary data                token or secret hash
  `JSONB`             Queryable JSON data        worker labels
  `TIMESTAMPTZ`       Timestamp with time zone   `last_seen_at`

Use `BIGINT` for byte counts because RAM values can exceed the range of
a 32-bit integer. Use `TIMESTAMPTZ` for timestamps representing real
moments in time.

## 4. SQL: the language used to work with PostgreSQL

SQL is used to define tables and query or modify data.

### Create a table

``` sql
CREATE TABLE workers (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    os TEXT NOT NULL,
    ram_total_bytes BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Insert a row

``` sql
INSERT INTO workers (id, hostname, os, ram_total_bytes)
VALUES ('wrk_demo1', 'my-laptop', 'darwin', 17179869184);
```

### Read rows

``` sql
SELECT id, hostname, os
FROM workers;
```

### Filter rows

``` sql
SELECT id, hostname
FROM workers
WHERE os = 'darwin';
```

### Update a row

``` sql
UPDATE workers
SET hostname = 'updated-laptop'
WHERE id = 'wrk_demo1';
```

### Delete a row

``` sql
DELETE FROM workers
WHERE id = 'wrk_demo1';
```

Always understand the `WHERE` clause before running an `UPDATE` or
`DELETE`. Without it, the statement can affect every row.

### Sort and limit

``` sql
SELECT id, hostname, created_at
FROM workers
ORDER BY created_at DESC
LIMIT 10;
```

### Count records

``` sql
SELECT COUNT(*)
FROM workers;
```

### Parameters, not string concatenation

Application code must use parameterized queries. Do not build SQL by
concatenating user-controlled values.

Conceptually:

``` sql
SELECT id, hostname
FROM workers
WHERE id = $1;
```

The Go PostgreSQL driver sends the value separately. This avoids SQL
injection and quoting bugs.

## 5. Indexes

An index is a data structure that helps PostgreSQL find rows without
scanning the entire table.

Example:

``` sql
CREATE INDEX workers_last_seen_idx
ON workers (last_seen_at);
```

Indexes are useful when a query frequently filters or sorts by a column.
They are not free: PostgreSQL must maintain them when rows change, and
they use disk space.

Do not add indexes to every column. Start with the queries the
application actually runs, then measure.

### Lesson 1 --- Database thinking tasks

Do these before writing application code:

1.  Explain the difference between a row and a column.
2.  Explain why `workers.id` should be a primary key.
3.  Explain why `tasks.worker_id` should be a foreign key.
4.  Explain why RAM bytes should use `BIGINT`.
5.  Explain why `last_seen_at` should use `TIMESTAMPTZ`.
6.  Write a `SELECT` query that finds workers whose OS is `linux`.
7.  Write an `UPDATE` query that changes one worker's hostname.
8.  Explain what could go wrong if that `UPDATE` has no `WHERE`.

**Checkpoint:** Write your answers in
`docs/learning-notes/database-basics.md`. Do not start Go database code
until you can explain these ideas.

------------------------------------------------------------------------

# Part IV --- Docker fundamentals

## 6. What Docker does

Docker packages and runs software in containers. A container is an
isolated process with its filesystem, network settings, and resource
boundaries. It shares the host machine's kernel; it is not a full
virtual machine.

You will use Docker first to run PostgreSQL locally. You do not need to
containerize the Go coordinator immediately.

### Important vocabulary

-   **Image:** a packaged template used to create containers.
-   **Container:** a running or stopped instance created from an image.
-   **Dockerfile:** instructions for building an image.
-   **Volume:** persistent storage managed by Docker.
-   **Port mapping:** makes a container port reachable through a host
    port.
-   **Network:** allows containers to communicate.
-   **Environment variable:** configuration passed to a process.
-   **Docker Compose:** describes and runs a group of related
    containers.

### Image versus container

An image is like a recipe/package. A container is an instance created
from it. You can create multiple containers from one image.

### Run PostgreSQL in a container

The build guide's initial development command is:

``` bash
docker run --name tc-pg \
  -e POSTGRES_PASSWORD=dev \
  -p 5432:5432 \
  -d postgres:16
```

Understand each part:

-   `docker run`: create and start a container.
-   `--name tc-pg`: give it a predictable name.
-   `-e POSTGRES_PASSWORD=dev`: set the initial database superuser
    password for local development.
-   `-p 5432:5432`: map host port 5432 to container port 5432.
-   `-d`: run in the background.
-   `postgres:16`: use the PostgreSQL 16 image.

**Security note:** `dev` is a local learning password only. Never use it
for a public server or production deployment. Do not expose a
development database to the public internet.

### Inspect the container

``` bash
docker ps
docker ps -a
docker logs tc-pg
docker inspect tc-pg
```

-   `docker ps` shows running containers.
-   `docker ps -a` also shows stopped containers.
-   `docker logs` shows container output.
-   `docker inspect` shows detailed configuration.

### Stop and start

``` bash
docker stop tc-pg
docker start tc-pg
```

Stopping a container does not normally delete it. Starting the same
container again reuses its configuration and attached storage.

### Execute a command inside the container

``` bash
docker exec -it tc-pg psql -U postgres
```

You are now in the PostgreSQL interactive shell (`psql`). Try:

``` sql
SELECT version();
\l
\dt
\q
```

-   `\l` lists databases.
-   `\dt` lists tables in the current database.
-   `\q` exits `psql`.

### Remove a container

``` bash
docker stop tc-pg
docker rm tc-pg
```

Be careful: removing a container is different from stopping it. If you
have not configured persistent storage, removing the container can
remove data stored only in its writable layer.

## 7. Docker volumes: keep database data

A database must keep its data when its container is replaced. A volume
stores data outside the container's writable layer.

For a fresh local setup, you can create a named volume:

``` bash
docker volume create tc-pg-data
```

Then run PostgreSQL with it:

``` bash
docker run --name tc-pg \
  -e POSTGRES_PASSWORD=dev \
  -p 5432:5432 \
  -v tc-pg-data:/var/lib/postgresql/data \
  -d postgres:16
```

The left side (`tc-pg-data`) is the Docker volume. The right side is the
data directory inside the container.

Inspect volumes:

``` bash
docker volume ls
docker volume inspect tc-pg-data
```

A volume is not a backup. It protects data from normal container
replacement, but it does not protect against every disk failure or
accidental deletion. Backups are a separate responsibility.

## 8. Docker networks

Containers on the same Docker network can reach each other by container
or service name.

For example, if a future Compose stack has services named `coordinator`
and `db`, the coordinator can connect to PostgreSQL using host `db` and
port `5432`.

Important distinction:

-   From your Mac, the published database address may be
    `localhost:5432`.
-   From another container in the same Compose network, use the database
    service name, such as `db:5432`.
-   `localhost` inside a container means that same container, not your
    Mac and not another container.

This distinction is a very common source of connection errors.

## 9. Docker Compose

Compose describes multiple services in a YAML file. You will eventually
use it to run the coordinator and PostgreSQL together, and later the web
UI and reverse proxy.

A small learning example:

``` yaml
services:
  db:
    image: postgres:16
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: local-dev-only
      POSTGRES_DB: teamcompute
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

volumes:
  postgres_data:
```

This example is for local development only. Before using it, learn what
each field means:

-   `services`: containers that make up the application.
-   `db`: the service name; other Compose services can use it as a
    hostname.
-   `image`: the container image.
-   `environment`: variables passed to PostgreSQL.
-   `ports`: host-to-container port mapping.
-   `volumes`: persistent data storage.
-   top-level `volumes`: declares the named volume.

Commands:

``` bash
docker compose up -d
docker compose ps
docker compose logs -f db
docker compose down
```

`docker compose down` removes the containers and network, but normally
keeps named volumes. `docker compose down -v` also removes declared
named volumes. Do not use `-v` if you need to keep your local database
data.

### Lesson 2 --- Docker tasks

1.  Check whether Docker is installed and running.
2.  Run the PostgreSQL container using the build-guide command.
3.  Use `docker ps` to find it.
4.  Read its logs.
5.  Open `psql` using `docker exec`.
6.  Run `SELECT version();`.
7.  Stop and start the container.
8.  Explain why a volume is important for a database.
9.  Explain the difference between `localhost:5432` on your Mac and
    `db:5432` from another container.

**Checkpoint:** Save the commands you ran and explain each Docker flag
in `docs/learning-notes/docker-basics.md`.

------------------------------------------------------------------------

# Part V --- Design the first database schema

## 10. Start with the data model, not Go code

The roadmap's first-version data model includes `workers`,
`join_tokens`, and `tasks`. The first persistence lesson focuses on
workers and join tokens. Tasks persistence comes later when the task
system is built.

Do not add every future table now. Build only what the current
application needs.

### Workers table

The roadmap defines these fields:

  --------------------------------------------------------------------------------------
  Column                  Type                                   Purpose
  ----------------------- -------------------------------------- -----------------------
  `id`                    `TEXT PRIMARY KEY`                     Stable worker ID

  `hostname`              `TEXT NOT NULL`                        Machine hostname

  `os`                    `TEXT NOT NULL`                        Operating system

  `arch`                  `TEXT NOT NULL`                        CPU architecture

  `cpu_model`             `TEXT`                                 CPU model, optional

  `cpu_cores`             `INT`                                  CPU core count

  `ram_total_bytes`       `BIGINT NOT NULL`                      Total RAM in bytes

  `labels`                `JSONB NOT NULL DEFAULT '{}'`          Worker labels

  `secret_hash`           `BYTEA NOT NULL`                       Hash of worker secret;
                                                                 never the secret

  `status`                `TEXT NOT NULL DEFAULT 'pending'`      Stored status

  `last_seen_at`          `TIMESTAMPTZ`                          Last
                                                                 coordinator-observed
                                                                 heartbeat

  `agent_version`         `TEXT`                                 Agent version

  `created_at`            `TIMESTAMPTZ NOT NULL DEFAULT now()`   Record creation time
  --------------------------------------------------------------------------------------

### Join tokens table

  -------------------------------------------------------------------------------
  Column                  Type                            Purpose
  ----------------------- ------------------------------- -----------------------
  `id`                    `BIGSERIAL PRIMARY KEY`         Database-generated row
                                                          ID

  `token_hash`            `BYTEA NOT NULL UNIQUE`         Hash of the join token

  `expires_at`            `TIMESTAMPTZ NOT NULL`          Expiration moment

  `max_uses`              `INT NOT NULL DEFAULT 1`        Maximum registrations

  `used_count`            `INT NOT NULL DEFAULT 0`        Number of successful
                                                          uses

  `labels`                `JSONB NOT NULL DEFAULT '{}'`   Labels applied to
                                                          registered workers
  -------------------------------------------------------------------------------

### Why store hashes instead of secrets?

A join token or worker secret is a credential. If the database is
leaked, storing the raw secret would expose it directly. Store a
cryptographic hash and compare hashes during authentication. Do not log
credentials.

A hash is not encryption. You do not decrypt it to recover the original
token. The client presents the token again; the server hashes the
presented value and compares it securely with the stored hash.

### A note about status

The in-memory registry calculates status using the age of
`last_seen_at`. The build guide describes:

-   never seen: pending
-   age under 15 seconds: online
-   age from 15 seconds up to but not including 45 seconds: stale
-   age 45 seconds or more: offline

Do not accidentally make the database's old `status` value override the
registry's time-based calculation during startup. Treat status recovery
as a deliberate application rule.

### Schema design questions

Before writing the migration, answer:

1.  Which columns must always have values?
2.  Which fields can be absent?
3.  Which values must be unique?
4.  Which data should be JSONB, and why?
5.  Why is `secret_hash` binary data?
6.  Why should a token have both `max_uses` and `used_count`?
7.  What should happen if two agents try to consume the final token use
    at exactly the same time?

**Checkpoint:** Draw or write a simple relationship diagram. At this
stage, `tasks.worker_id` references `workers.id`; join tokens are
consumed during registration.

------------------------------------------------------------------------

# Part VI --- Go and PostgreSQL

## 11. The driver and connection pool

The build guide selects `pgx`:

``` text
github.com/jackc/pgx/v5/pgxpool
```

A driver lets Go communicate with PostgreSQL. `pgxpool` manages a pool
of database connections so the application can reuse connections rather
than opening a new connection for every query.

### Why use a connection pool?

Opening a new database connection for every request is expensive. A
pool:

-   reuses established connections;
-   limits how many connections the application opens;
-   manages connection acquisition and release;
-   provides context-aware operations.

A pool is not a cache of query results. It is a manager of database
connections.

### Context and timeouts

Go's `context.Context` carries cancellation and deadlines through a call
chain.

Database operations should accept a context so they can stop when the
caller is cancelled or a timeout expires.

Conceptual example:

``` go
ctx, cancel := context.WithTimeout(
    context.Background(),
    5*time.Second,
)
defer cancel()

// Pass ctx to a database operation.
```

Why this matters:

-   the database may be unavailable;
-   a query may become slow;
-   the coordinator may be shutting down;
-   the request that started the operation may be cancelled.

Do not use `context.Background()` deep inside every repository method.
Accept the caller's context and pass it through.

## 12. Separation of concerns: registry, interface, PostgreSQL implementation

The registry should own worker behavior and in-memory state. The store
should own durable persistence.

Conceptual dependency direction:

``` text
HTTP handler
    |
    v
Registry
    |
    v
Store interface
    |
    +------> PostgreSQL implementation
    |
    +------> Fake/in-memory implementation for tests
```

The registry should not import `pgx` or write SQL. That would tightly
couple business logic to one database implementation.

### Store interface from the build guide

The build guide specifies this contract:

``` go
type Store interface {
    SaveWorker(ctx context.Context, worker registry.Worker) error
    LoadWorkers(ctx context.Context) ([]registry.Worker, error)
    TouchLastSeen(ctx context.Context, seen map[string]time.Time) error

    CreateToken(
        ctx context.Context,
        hash []byte,
        expires time.Time,
        maxUses int,
        labels map[string]string,
    ) error

    ConsumeToken(
        ctx context.Context,
        hash []byte,
    ) (labels map[string]string, err error)
}
```

This is a contract, not a complete implementation. Match it carefully to
the actual types in your repository before coding. If your existing
`Worker` or token model differs, stop and inspect it rather than forcing
mismatched types into the interface.

### What each method means

-   `SaveWorker`: insert or update a worker on registration or a
    meaningful durable change.
-   `LoadWorkers`: retrieve persisted workers during coordinator
    startup.
-   `TouchLastSeen`: batch-update the latest heartbeat timestamps.
-   `CreateToken`: store a token hash, expiry, usage limit, and labels.
-   `ConsumeToken`: atomically consume one valid token use and return
    its labels.

### Why an interface?

The registry can depend on behavior rather than a concrete PostgreSQL
type. This makes it easier to test registry behavior without a real
database and makes storage replaceable.

Do not create interfaces for every struct automatically. Create an
interface when it gives a real boundary or makes a dependency easier to
test.

## 13. SQL transactions and atomic token consumption

A transaction groups database operations into one logical unit. Either
the transaction commits, or its changes are rolled back.

For example, token consumption must not allow two concurrent
registrations to both use the final available token use.

A naïve sequence is unsafe:

1.  Read `used_count`.
2.  Check `used_count < max_uses`.
3.  Update `used_count = used_count + 1`.

Two concurrent requests can both read the same old count and both
believe a use remains.

Instead, perform the condition and update atomically in the database.
One possible SQL pattern is:

``` sql
UPDATE join_tokens
SET used_count = used_count + 1
WHERE token_hash = $1
  AND expires_at > now()
  AND used_count < max_uses
RETURNING labels;
```

If no row is returned, the token is missing, expired, or exhausted. The
application should return a safe error without exposing secret material.

This is an important lesson: concurrency correctness should be enforced
at the database boundary, not only by an in-memory `if` statement.

## 14. Error handling

Do not ignore database errors.

Think about these categories:

-   connection failure;
-   query timeout or cancellation;
-   duplicate key / unique constraint violation;
-   missing row;
-   invalid data;
-   transaction failure.

Return errors with useful context, but do not include passwords, tokens,
or secrets in error messages or logs.

Use error wrapping where it helps identify the operation:

``` go
return fmt.Errorf("load workers: %w", err)
```

Do not turn every error into a generic success or silently continue if
persistence is required for correctness.

------------------------------------------------------------------------

# Part VII --- Migrations

## 15. What is a migration?

A migration is a versioned database schema change. Instead of manually
creating tables on every machine, the project stores SQL changes in
files and applies them in order.

Example project layout:

``` text
migrations/
  001_init.sql
  002_models.sql
  003_tasks.sql
```

The build guide asks for an initial migration containing `workers` and
`join_tokens`. Later migrations add models and tasks when those features
are implemented.

### Why migrations matter

Imagine your application runs on two computers. Both need the same
schema. A migration provides a repeatable way to create or change that
schema.

A migration tool records which versions have been applied. The guide
allows either `goose` or `golang-migrate`; choose one and use it
consistently.

### Migration rules

-   Give each migration a unique, ordered version.
-   Keep migrations committed in Git.
-   Do not edit an already-applied migration casually.
-   Add a new migration for a later schema change.
-   Test migrations against a fresh database.
-   Test startup against a database that already has the migrations
    applied.
-   Know how rollback works for the tool you select.
-   Never assume a destructive migration is safe just because it
    succeeds locally.

### Your first migration task

Create `migrations/001_init.sql` with the first-version `workers` and
`join_tokens` schema from Part V.

Do not add `tasks` yet unless the current implementation already needs
task persistence. The build guide's first PostgreSQL persistence step is
about workers and join tokens.

------------------------------------------------------------------------

# Part VIII --- Docker + database development workflow

## 16. Local development configuration

The coordinator should receive its database connection string from
configuration, usually an environment variable. Do not hardcode
credentials in Go source.

A local-only example:

``` bash
export DATABASE_URL='postgres://postgres:dev@localhost:5432/postgres'
```

The build guide's store test command uses:

``` bash
TC_TEST_DB=postgres://postgres:dev@localhost:5432/postgres \
  go test ./internal/coordinator/store -v
```

Keep the test database separate from any important data. Integration
tests may create, update, or delete rows.

### Configuration checklist

-   Read the database URL from an environment variable.
-   Fail clearly if required configuration is missing.
-   Do not print the full URL if it contains credentials.
-   Set a connection timeout.
-   Close the pool during graceful shutdown.
-   Use a different database or schema for tests when practical.

## 17. Docker Compose for local PostgreSQL

Once you understand `docker run`, move the local database setup into a
Compose file. Start with only PostgreSQL; do not add the coordinator,
web UI, or Caddy yet.

Your task is to create a small `compose.yaml` that:

-   runs PostgreSQL 16;
-   defines a database, user, and local development password;
-   maps port 5432 for local tools;
-   uses a named volume for database data;
-   has a health check if you understand how it works.

Then prove that data survives:

1.  Start the database.
2.  Create a test table and insert one row.
3.  Stop the Compose stack.
4.  Start it again.
5.  Query the row.
6.  Explain why the row still exists.

Useful commands:

``` bash
docker compose up -d
docker compose ps
docker compose logs -f db
docker compose down
docker compose up -d
```

Avoid `docker compose down -v` during this exercise unless you
intentionally want to delete the named volume and all its database data.

------------------------------------------------------------------------

# Part IX --- Testing the persistence layer

## 18. Unit tests versus integration tests

A **unit test** checks a small piece of logic in isolation. A registry
test with a fake store can test status rules without starting
PostgreSQL.

An **integration test** checks that components work together. A store
integration test uses a real PostgreSQL database and verifies that SQL,
schema, driver, and application code agree.

Both are necessary. A unit test cannot prove that your SQL schema is
correct. An integration test is usually slower and should not replace
fast unit tests.

### Store tests to write

At minimum, plan tests for:

1.  Saving a worker and loading it again.
2.  Updating a worker without creating a duplicate.
3.  Touching `LastSeen` for multiple workers in one call.
4.  Creating a join token.
5.  Consuming a valid token.
6.  Rejecting an expired token.
7.  Rejecting an exhausted token.
8.  Ensuring concurrent consumption cannot exceed `max_uses`.
9.  Ensuring no plain secret is stored.

Use a clean test database. Tests should not depend on execution order or
data left behind by a previous run.

### Test the atomic token rule

For a token with `max_uses = 1`, start two goroutines that attempt to
consume it concurrently.

Expected result:

-   exactly one succeeds;
-   the other receives a safe failure;
-   `used_count` is 1, not 2.

This is a concurrency test, not merely a normal sequential test.

## 19. End-to-end restart test

The build guide's acceptance test is important because persistence
exists to survive process restarts.

After connecting three agents:

1.  Confirm all three worker IDs appear.
2.  Stop the coordinator gracefully.
3.  Start it again using the same database.
4.  Immediately query the worker list.
5.  Confirm the same three IDs exist, but they are not falsely marked
    online.
6.  Wait for heartbeats.
7.  Confirm the same workers become online again without re-registering.
8.  Query PostgreSQL and verify the rows remain.

The build guide uses commands like:

``` bash
curl -s localhost:9090/v1/workers | jq -r \
  '.[] | "\(.id) \(.hostname) \(.status)"'

docker exec -it tc-pg psql -U postgres \
  -c "select id,hostname,last_seen_at from workers;"
```

Use the actual endpoint and port from your current repository if they
differ.

------------------------------------------------------------------------

# Part X --- The implementation sequence

## 20. Do not jump straight into persistence

Your current branch has been identified as `step-1.4-auth-heartbeat`.
The main build-along guide places PostgreSQL persistence at **Step
1.12**, after the intervening worker connection, registry, token, and
heartbeat work.

Therefore, use this order:

### Stage A --- Prepare your environment

-   [x] Inspect `git status`, current branch, and recent commits.
-   [x] Inspect and safely handle `.DS_Store`.
-   [x] Confirm Docker is installed and running.
-   [x] Learn the Docker commands in Part IV.
-   [x] Start a local PostgreSQL container.
-   [x] Complete the SQL exercises.
-   [x] Save your notes.

### Stage B --- Finish prerequisites in the main build guide

-   [ ] Continue the remaining steps between your current point and Step
    1.12.
-   [ ] Keep the current tests passing.
-   [ ] Commit each completed, tested step.
-   [ ] Do not start persistence until the registry and token behavior
    that persistence must store are stable.

### Stage C --- Step 1.12: PostgreSQL persistence

Follow this order:

1.  Inspect the existing `Worker`, registry, token, and coordinator
    types.
2.  Design the initial schema.
3.  Create `migrations/001_init.sql`.
4.  Choose and configure `goose` or `golang-migrate`.
5.  Add `pgx/v5/pgxpool`.
6.  Define the storage interface in the appropriate package.
7.  Write store tests.
8.  Implement worker save/load operations.
9.  Implement batched `LastSeen` updates.
10. Implement token creation and atomic consumption.
11. Load persisted workers during coordinator startup.
12. Add periodic write-behind flushing and a final shutdown flush.
13. Run unit and integration tests.
14. Perform the coordinator restart acceptance test.

### Stage D --- Docker Compose

-   [ ] Move local PostgreSQL setup into Compose.
-   [ ] Prove data survives container recreation.
-   [ ] Add the coordinator service only after its configuration is
    ready.
-   [ ] Add the web UI and reverse proxy later, when those parts are
    ready.
-   [ ] Document environment variables and volumes.

------------------------------------------------------------------------

# Part XI --- Detailed task cards

Use these as your task list. Complete one card, test it, and commit
before moving on.

## Task 0 --- Repository safety check

**Goal:** Start from a clean understanding of the current working tree.

Run:

``` bash
git status --short
git branch --show-current
git log -5 --oneline
find docs -maxdepth 3 -type f 2>/dev/null | sort
```

**Do:** - Identify tracked and untracked files. - Inspect the contents
of `docs/` before deciding what to do with it. - Add `.DS_Store` to
`.gitignore` if needed. - Do not stage unrelated files.

**Pass when:** You can explain every changed or untracked file and know
which branch you are on.

## Task 1 --- Docker and PostgreSQL setup

**Goal:** Run PostgreSQL locally and connect to it.

**Do:** - Check `docker --version` and `docker compose version`. - Start
PostgreSQL using the command in Part IV. - Connect using
`docker exec`. - Run `SELECT version();`. - Stop and start the
container.

**Pass when:** You can connect after restarting the container and
explain the commands.

## Task 2 --- SQL practice

**Goal:** Become comfortable with basic relational operations.

**Do:** - Create a temporary practice table. - Insert at least three
workers. - Select all workers. - Filter by OS. - Update one hostname. -
Delete one practice row. - Count the remaining rows.

**Pass when:** You can write each query without copying it and explain
its effect.

## Task 3 --- Schema design

**Goal:** Understand the workers and join-token data model.

**Do:** - Review the schema in Part V. - Explain each field and type. -
Identify required fields and uniqueness rules. - Explain atomic token
consumption. - Write a draft schema on paper or in notes.

**Pass when:** You can explain why each field exists before writing the
migration.

## Task 4 --- Initial migration

**Goal:** Create a reproducible database schema.

**Do:** - Choose `goose` or `golang-migrate`. - Add
`migrations/001_init.sql`. - Create only the tables required for this
persistence step. - Apply the migration to a fresh local database. -
Inspect the tables with `psql`.

**Pass when:** A fresh database can be created from the migration and
the resulting schema matches your design.

## Task 5 --- Store interface

**Goal:** Separate registry logic from SQL.

**Do:** - Inspect the actual registry types. - Define the store
contract. - Decide which package should own the interface. - Ensure
registry code does not import `pgx`. - Add a fake store for tests if
needed.

**Pass when:** You can explain the dependency direction and compile the
relevant packages.

## Task 6 --- PostgreSQL worker store

**Goal:** Persist and retrieve worker records.

**Do:** - Create a connection pool. - Implement `SaveWorker`. -
Implement `LoadWorkers`. - Use parameterized SQL. - Handle and wrap
errors. - Write integration tests.

**Pass when:** A worker can be saved, the process can stop, and the
worker can be loaded from PostgreSQL.

## Task 7 --- Batched heartbeat persistence

**Goal:** Avoid a database write for every heartbeat.

**Do:** - Track which worker timestamps need persistence. - Flush dirty
timestamps in one batch operation. - Flush periodically (about every 15
seconds, as specified by the build guide). - Flush once during graceful
shutdown. - Handle failed flushes without silently losing track of dirty
data.

**Pass when:** Multiple dirty workers can be updated in one database
round trip and tests cover failure behavior.

## Task 8 --- Join-token persistence

**Goal:** Store only token hashes and consume uses safely.

**Do:** - Store the hash, expiry, maximum uses, current use count, and
labels. - Implement atomic consumption in PostgreSQL. - Test missing,
expired, exhausted, and valid tokens. - Add a concurrent-consumption
test. - Check that no plain token is written to the database or logs.

**Pass when:** A one-use token cannot be consumed twice, even by
concurrent requests.

## Task 9 --- Startup recovery

**Goal:** Restore durable worker records after coordinator restart.

**Do:** - Load workers from the store during startup. - Add them to the
in-memory registry. - Ensure stale timestamps are interpreted
correctly. - Confirm a new heartbeat restores online status.

**Pass when:** Worker IDs survive a restart and workers become online
again only after they heartbeat.

## Task 10 --- Compose development environment

**Goal:** Make local database setup repeatable.

**Do:** - Create a Compose file for PostgreSQL. - Configure a named
volume. - Start, stop, and recreate the container. - Prove a test row
survives. - Document how to reset the local database safely.

**Pass when:** Another developer can start the local database using the
documented commands.

------------------------------------------------------------------------

# Part XII --- Engineering patterns to learn

## 21. Patterns and decisions

### Repository / store pattern

The store hides persistence details behind methods that represent
application operations. The registry does not need to know how SQL is
written.

**Good reason to use it:** Keep business logic independent of the
database and make testing easier.

**Bad use:** Creating many tiny interfaces with no real boundary or
alternative implementation.

### Dependency injection

A component receives a dependency instead of constructing it internally.

For example, the coordinator receives a `Store` when it is created. This
makes it possible to pass a fake store in tests and a PostgreSQL store
in production.

### Connection pooling

Reuse a bounded set of database connections instead of creating one per
operation.

### Transactions

Group related database changes so they succeed or fail together.

### Atomic conditional update

Let PostgreSQL enforce a condition and update in one operation,
especially for limited-use tokens.

### Write-behind batching

Keep frequently changing state in memory and persist selected updates
periodically in batches.

### Database migrations

Track schema evolution as ordered, repeatable changes committed to
source control.

### Integration testing

Test the real boundary between Go and PostgreSQL, not just mocked
behavior.

------------------------------------------------------------------------

# Part XIII --- Common mistakes to avoid

1.  **Putting SQL in HTTP handlers.** Keep request handling, registry
    logic, and persistence responsibilities separate.
2.  **Letting the registry import `pgx`.** The registry should depend on
    the store contract, not the PostgreSQL implementation.
3.  **Writing every heartbeat to the database.** Use the planned batch
    strategy.
4.  **Storing plain secrets.** Store hashes only; never log credentials.
5.  **Using string concatenation for SQL.** Use parameterized queries.
6.  **Ignoring errors.** Database failures are real application
    failures.
7.  **Using `localhost` from the wrong container.** Inside a container,
    localhost refers to that container.
8.  **Forgetting volumes.** Container replacement should not erase the
    database.
9.  **Treating volumes as backups.** Keep separate backups for important
    data.
10. **Running destructive Compose commands casually.**
    `docker compose down -v` removes named volumes.
11. **Editing old migrations after they have been applied.** Prefer a
    new migration.
12. **Testing only with mocks.** Use PostgreSQL integration tests for
    SQL behavior.
13. **Persisting computed online status as unquestioned truth.**
    Recalculate it from `LastSeen` on startup.
14. **Doing too much in one branch.** Keep each branch focused on one
    coherent feature.
15. **Committing secrets or unrelated files.** Inspect `git diff` and
    `git status` before every commit.

------------------------------------------------------------------------

# Part XIV --- Definition of done

The database and Docker learning milestone is complete when you can
demonstrate all of the following:

-   [ ] Explain tables, rows, columns, primary keys, foreign keys,
    constraints, and indexes.
-   [ ] Write basic `SELECT`, `INSERT`, `UPDATE`, and `DELETE`
    statements.
-   [ ] Explain PostgreSQL types used by this project.
-   [ ] Run PostgreSQL locally in Docker.
-   [ ] Explain images, containers, ports, volumes, networks, and
    Compose.
-   [ ] Prove database data survives container recreation.
-   [ ] Explain why Go uses a connection pool.
-   [ ] Explain why the registry depends on a store interface.
-   [ ] Explain how migrations work.
-   [ ] Save and load workers through PostgreSQL.
-   [ ] Batch heartbeat persistence rather than writing every heartbeat.
-   [ ] Consume join tokens atomically.
-   [ ] Recover workers after a coordinator restart.
-   [ ] Pass unit and PostgreSQL integration tests.
-   [ ] Keep credentials out of source control and database plaintext
    fields.
-   [ ] Commit and push the completed work with clear messages.

------------------------------------------------------------------------

# First session --- Start here

Do **not** begin by writing the PostgreSQL store.

Start with these three actions:

1.  Run the repository safety commands from Task 0 and inspect the
    untracked files.
2.  Check Docker availability and start the local PostgreSQL container
    from Task 1.
3.  Connect to `psql` and run:

``` sql
SELECT version();
```

Then complete the first SQL practice task.

When you return for tutoring, send:

``` text
I completed Task 0 and Task 1.

Current branch:
git status --short:
Docker version:
PostgreSQL version:
What I learned:
What confused me:
```

We will review your work, fix issues, and unlock the next task. The goal
is not just to make the platform work; it is to understand the
engineering decisions well enough to build and maintain it yourself.

# After Session 1 — Continue the Roadmap

## Session 2 — Docker Fundamentals & PostgreSQL

**Goal:** Understand how Docker runs PostgreSQL, how containers work, and how data persists.

### Task 3 — Understand Docker

Before writing commands, understand:

- [ ] What is a container?
- [ ] What is an image?
- [ ] Why PostgreSQL runs inside a container?
- [ ] What is port mapping?
- [ ] What is a volume?
- [ ] What happens when a container is deleted?
- [ ] Why do we need persistent storage?

### Practice

- [ ] Inspect the PostgreSQL container
- [ ] View its logs
- [ ] Stop it
- [ ] Start it again
- [ ] Connect to it again
- [ ] Understand the difference between stopping and deleting
- [ ] Understand where database data lives

---

## Session 3 — SQL & Database Design

**Goal:** Learn the SQL and database concepts we need for Team Compute Platform.

### Task 4 — SQL Fundamentals

Practice:

- [ ] `CREATE TABLE`
- [ ] Data types
- [ ] `PRIMARY KEY`
- [ ] `NOT NULL`
- [ ] `UNIQUE`
- [ ] `DEFAULT`
- [ ] `INSERT`
- [ ] `SELECT`
- [ ] `WHERE`
- [ ] `UPDATE`
- [ ] `DELETE`

### Task 5 — Database Relationships

Understand:

- [ ] Primary keys
- [ ] Foreign keys
- [ ] One-to-one relationships
- [ ] One-to-many relationships
- [ ] Many-to-many relationships
- [ ] Why relational databases enforce constraints

### Practice

Design a simple database for workers.

```text
Worker:
- ID
- Hostname
- Operating system
- CPU
- CPU cores
- RAM
- Last seen
- Status
```

Try creating this table yourself before implementing it in the project.

---

## Session 4 — PostgreSQL for Our Project

**Goal:** Understand the database structure we need before implementing persistence.

### Task 6 — Design the Workers Table

- [ ] Decide what columns we need
- [ ] Choose appropriate PostgreSQL data types
- [ ] Understand why RAM should use `BIGINT`
- [ ] Understand timestamps
- [ ] Understand JSONB
- [ ] Understand constraints
- [ ] Write the SQL schema
- [ ] Test the schema manually

### Task 7 — Design Join Tokens

Understand why agents need secure registration.

- [ ] Why do agents need authentication?
- [ ] What is a join token?
- [ ] Why shouldn't we store raw tokens?
- [ ] What is token expiration?
- [ ] What does `max_uses` mean?
- [ ] Why must token consumption be atomic?

Design the table:

```text
JoinToken:
- Token hash
- Expiration
- Maximum uses
- Used count
- Labels
```

---

## Session 5 — Database Migrations

**Goal:** Learn how we manage database schema changes safely.

### Task 8 — Migration Fundamentals

Understand:

- [ ] What is a migration?
- [ ] Why shouldn't we manually change production databases?
- [ ] What is migration versioning?
- [ ] Why migrations are committed to Git?
- [ ] What happens when a migration fails?

### Practice

- [ ] Install Goose
- [ ] Create your first migration
- [ ] Write the workers table schema
- [ ] Run the migration
- [ ] Inspect the database
- [ ] Understand the migration history
- [ ] Practice rolling back if appropriate

---

## Session 6 — Go & PostgreSQL

**Goal:** Connect our Go application to PostgreSQL.

### Task 9 — Database Connection

Understand:

- [ ] What is a database connection?
- [ ] What is a connection pool?
- [ ] Why do we use `pgxpool`?
- [ ] What is a context?
- [ ] Why should database operations have timeouts?
- [ ] Why should we use parameterized queries?

### Implementation

Create a database connection package.

Requirements:

- [ ] Load the database URL from environment variables
- [ ] Connect using `pgxpool`
- [ ] Check database connectivity
- [ ] Handle connection errors
- [ ] Close the pool gracefully
- [ ] Test the connection

---

## Session 7 — Store Interface & Architecture

**Goal:** Understand how PostgreSQL fits into our existing architecture.

### Task 10 — Store Interface

Before writing implementation, understand:

```text
Coordinator
    |
    v
Registry
    |
    v
Store Interface
    |
    v
PostgreSQL Implementation
```

Questions:

- Why should the registry not directly depend on PostgreSQL?
- What is an interface?
- What does dependency injection mean?
- Why are we separating storage from business logic?
- How will testing become easier?

### Implementation

- [ ] Inspect current Registry
- [ ] Inspect Worker struct
- [ ] Identify what information needs persistence
- [ ] Design the Store interface
- [ ] Review the interface before implementing it
- [ ] Implement PostgreSQL Store

---

## Session 8 — Worker Persistence

**Goal:** Persist worker information across coordinator restarts.

### Task 11 — Save Workers

- [ ] Implement `SaveWorker`
- [ ] Use parameterized SQL
- [ ] Handle errors properly
- [ ] Understand UPSERT
- [ ] Test worker creation
- [ ] Test worker updates

### Task 12 — Load Workers

- [ ] Implement `LoadWorkers`
- [ ] Load workers when coordinator starts
- [ ] Understand why previously online workers should not automatically be considered online
- [ ] Restore them as offline until they heartbeat
- [ ] Test startup behavior

---

## Session 9 — Heartbeat Persistence

**Goal:** Persist last-seen timestamps efficiently.

### Task 13 — LastSeen

Understand:

- Why heartbeats happen frequently
- Why we don't necessarily write every heartbeat to PostgreSQL
- What write-behind means
- What batching means
- What data loss window means

Implement:

- [ ] Collect LastSeen updates
- [ ] Batch updates
- [ ] Flush periodically
- [ ] Flush on graceful shutdown
- [ ] Handle database errors
- [ ] Test race conditions

---

## Session 10 — Secure Token Storage

**Goal:** Implement secure worker registration.

### Task 14 — Token Creation

- [ ] Generate a secure random token
- [ ] Hash the token
- [ ] Store only the hash
- [ ] Set expiration
- [ ] Set usage limit
- [ ] Store labels

### Task 15 — Atomic Token Consumption

Understand:

- What is a race condition?
- What is atomicity?
- Why can two workers accidentally consume the same token?
- Why is a single conditional SQL statement safer?

Implement:

- [ ] Consume token atomically
- [ ] Reject expired tokens
- [ ] Reject exhausted tokens
- [ ] Return labels
- [ ] Test concurrent requests

---

## Session 11 — Docker Compose

**Goal:** Run the development infrastructure consistently.

### Task 16 — Docker Compose

Understand:

- What is Compose?
- Why multiple services can share a network?
- What is service discovery?
- Why does a Go application inside Docker use `db` instead of `localhost`?
- What are environment variables?
- What are health checks?

Implement:

- [ ] Create `compose.yaml`
- [ ] Configure PostgreSQL
- [ ] Add persistent volume
- [ ] Configure health check
- [ ] Configure environment variables
- [ ] Start the database with Compose
- [ ] Connect the Go coordinator

---

## Session 12 — Testing & Final Integration

**Goal:** Make sure the persistence system works correctly.

### Task 17 — Database Testing

- [ ] Test database connection
- [ ] Test migrations
- [ ] Test saving workers
- [ ] Test loading workers
- [ ] Test updating LastSeen
- [ ] Test token creation
- [ ] Test token consumption
- [ ] Test concurrent token consumption
- [ ] Test coordinator restart
- [ ] Run `go test ./...`
- [ ] Run `go test -race ./...`

---

# Final Definition of Done

The database and Docker integration is complete when:

- [ ] PostgreSQL runs through Docker
- [ ] Database data persists using a volume
- [ ] Migrations work
- [ ] Coordinator connects to PostgreSQL
- [ ] Workers persist
- [ ] Workers load after coordinator restart
- [ ] LastSeen is persisted
- [ ] Join tokens are stored securely
- [ ] Tokens are consumed atomically
- [ ] Tests pass
- [ ] Docker Compose runs the local infrastructure
- [ ] I understand the architecture and can explain the decisions

---

# My Learning Rule

For every task:

1. Understand the problem.
2. Learn the concepts needed.
3. Explore the existing code.
4. Design the solution together.
5. Implement one small step.
6. Explain the code.
7. Test it.
8. Review mistakes.
9. Commit the work.

**Don't rush to finish the code. Build the understanding that allows you to build the next system without following a guide.**
