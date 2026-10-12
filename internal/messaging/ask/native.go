package ask

import "github.com/cloudvane/litefactory/internal/messaging/response"

// ChatRunner is the shared verified response-only provider contract.
type ChatRunner = response.ChatRunner

// MeterWriter is the shared repository response usage ledger contract.
type MeterWriter = response.MeterWriter

// NativeModel reuses shared isolated support for ask, feature and presentation replies.
type NativeModel = response.NativeModel
