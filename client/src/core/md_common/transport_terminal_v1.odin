package md_common

import "mr:ports"

ws_fault_action :: proc(category: ports.MD_WS_Error_Category, allow_legacy_ws: bool) -> ports.MD_WS_Error_Action {
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
		if allow_legacy_ws {
			return .Downgrade
		}
		return .Stop
	case .BackpressureDrop:
		return .Resync
	case .None:
	}
	return .Retry
}
