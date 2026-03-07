package ui

Global_Command :: enum u8 {
	None,
	Open_Connection_Manager,
	Toggle_Stream_Picker,
	Resync_Active_Stream,
	Toggle_Telemetry_HUD,
	Capture_Runtime_Snapshot, // S46
}

global_command_from_keys :: proc(ctrl_down: bool, key_k_pressed: bool, key_g_pressed: bool, key_r_pressed: bool, key_h_pressed: bool, key_d_pressed: bool = false) -> Global_Command {
	if ctrl_down && key_k_pressed do return .Open_Connection_Manager
	if key_g_pressed do return .Toggle_Stream_Picker
	if ctrl_down && key_r_pressed do return .Resync_Active_Stream
	if ctrl_down && key_h_pressed do return .Toggle_Telemetry_HUD
	if ctrl_down && key_d_pressed do return .Capture_Runtime_Snapshot
	return .None
}
