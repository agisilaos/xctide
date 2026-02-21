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

## `xctide xctest <args...>`

Runs `xcrun xctest <args...>` for direct `xctest` invocation.

Examples:

- `xctide xctest /path/to/YourTests.xctest`
- `xctide xctest -XCTest YourSuite/testExample /path/to/YourTests.xctest`

Notes:

- this is an explicit, low-level runner path; no xcodebuild config auto-detection is applied.
