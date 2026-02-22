# Tooling Passthrough Commands

`xctide` supports explicit passthrough commands for Apple developer tooling.

## `xctide xcrun <args...>`

Runs `xcrun` directly with the provided arguments.

Examples:

- `xctide xcrun simctl list devices available`
- `xctide xcrun -- simctl list runtimes`

Notes:

- `xctide` forwards stdout/stderr/stdin from the child process.
- exit code is forwarded from the underlying `xcrun` process.

### `xctrace` via `xcrun`

`xctide` supports `xctrace` by forwarding arguments through `xcrun`:

- `xctide xcrun xctrace version`
- `xctide xcrun xctrace list templates`
- `xctide xcrun xctrace list devices`
- `xctide xcrun xctrace record --template 'Time Profiler' --all-processes --time-limit 5s`
- `xctide xcrun xctrace export --input recording.trace --toc`

Notes:

- `xctide` does not reinterpret `xctrace` flags; all arguments are passed through as-is.
- for launch-mode tracing, include `--launch --` exactly as required by `xctrace`:
  - `xctide xcrun xctrace record --template 'Time Profiler' --launch -- /path/to/tool arg1`

## `xctide xctest <args...>`

Runs `xcrun xctest <args...>` for direct `xctest` invocation.

Examples:

- `xctide xctest /path/to/YourTests.xctest`
- `xctide xctest -XCTest YourSuite/testExample /path/to/YourTests.xctest`

Notes:

- this is an explicit, low-level runner path; no xcodebuild config auto-detection is applied.
- `xctide xctest --help` prints wrapper-level help/examples to avoid noisy raw tool diagnostics.
