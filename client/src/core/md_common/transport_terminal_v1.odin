package md_common

import "mr:ports"
import "mr:util"

// Map server-provided action_hint to deterministic WS fault action.
// Returns (.None, false) if hint is Unspecified (caller should use legacy fallback).
action_hint_to_ws_fault :: proc(hint: util.MR_Action_Hint) -> (ports.MD_WS_Error_Action, bool) {
	switch hint {
	case .Retry:       return .Retry, true
	case .Reconnect:   return .Retry, true
	case .Resubscribe: return .Resync, true
	case .Resync:      return .Resync, true
	case .None:        return .None, true
	case .Unspecified:
	}
	return .None, false
}

ws_fault_action :: proc(category: ports.MD_WS_Error_Category) -> ports.MD_WS_Error_Action {
	switch category {
	case .AuthDenied:
		return .Stop
	case .HandshakeFailed:
		return .Retry
	case .ServerClosed:
		return .Retry
	case .ProtocolError:
		return .Resync
	case .Timeout:
		return .Stop
	case .BackpressureDrop:
		return .Resync
	case .None:
	}
	return .Retry
}
