# Config Validation File Split Design

## Goal

Move the private configuration normalization and validation pipeline from `internal/config/config.go` to `internal/config/validate.go` without changing behavior.

## File boundaries

`internal/config/config.go` keeps:

- protocol constants and configuration structs;
- `Load`, which decodes configuration and invokes validation;
- `Current`, which selects the active platform;
- `currentPlatformNotFoundError`, which only supports `Current`.

`internal/config/validate.go` owns the complete private normalization and validation pipeline:

- `Config.normalizeAndValidate`;
- `BotConfig.normalizeAndValidate`;
- `Options.normalize`;
- `Options.validate`;
- `Options.normalizeBaseURL`.

Keeping normalization beside validation preserves the existing execution order and avoids splitting one pipeline across multiple files.

## Behavior and errors

This is a file-only refactor. Function signatures, validation order, accepted values, normalization results, error messages, and credential-safety behavior remain unchanged.

## Documentation and verification

Update the README package tree so `config.go` describes loading and active-platform selection, while `validate.go` describes normalization and validation. Use the existing `internal/config/config_test.go` coverage as the behavior contract, then run the configuration package tests and the full Go test suite.
