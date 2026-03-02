package streams

STREAM_CAP :: 64

Stream_Registry :: struct {
	handles:                 [STREAM_CAP]Stream_Handle,
	count:                   int,
	has_active:              bool,
	active_stream_id_buf:    [STREAM_ID_CAP]u8,
	active_stream_id_len:    u8,
	pause_when_unsubscribed: bool,
}

registry_init :: proc(reg: ^Stream_Registry, pause_when_unsubscribed: bool = true) {
	if reg == nil do return
	reg^ = {}
	reg.pause_when_unsubscribed = pause_when_unsubscribed
}

registry_find :: proc(reg: ^Stream_Registry, sid: string) -> int {
	if reg == nil do return -1
	if len(sid) == 0 do return -1
	for i in 0 ..< len(reg.handles) {
		h := &reg.handles[i]
		if !h.used do continue
		if sid == stream_id(h) do return i
	}
	return -1
}

@(private = "file")
registry_alloc :: proc(reg: ^Stream_Registry) -> ^Stream_Handle {
	if reg == nil do return nil
	for i in 0 ..< len(reg.handles) {
		if reg.handles[i].used do continue
		h := &reg.handles[i]
		h^ = {}
		h.used = true
		h.status.state = .Offline
		reg.count += 1
		return h
	}
	return nil
}

registry_get_or_create :: proc(reg: ^Stream_Registry, stream_id: string, venue: string, symbol: string, market_type: string = "") -> ^Stream_Handle {
	if reg == nil do return nil
	if len(stream_id) == 0 do return nil
	if idx := registry_find(reg, stream_id); idx >= 0 {
		h := &reg.handles[idx]
		set_stream_identity(h, stream_id, venue, symbol, market_type)
		return h
	}
	h := registry_alloc(reg)
	if h == nil do return nil
	set_stream_identity(h, stream_id, venue, symbol, market_type)
	return h
}

registry_set_active :: proc(reg: ^Stream_Registry, stream_id: string) {
	if reg == nil do return
	if len(stream_id) == 0 {
		reg.has_active = false
		reg.active_stream_id_len = 0
		return
	}
	n := min(len(stream_id), len(reg.active_stream_id_buf))
	for i in 0 ..< n {
		reg.active_stream_id_buf[i] = stream_id[i]
	}
	reg.active_stream_id_len = u8(n)
	reg.has_active = true
}

registry_active_stream_id :: proc(reg: ^Stream_Registry) -> string {
	if reg == nil || !reg.has_active || reg.active_stream_id_len == 0 do return ""
	n := int(reg.active_stream_id_len)
	if n > len(reg.active_stream_id_buf) do n = len(reg.active_stream_id_buf)
	return string(reg.active_stream_id_buf[:n])
}

registry_active :: proc(reg: ^Stream_Registry) -> ^Stream_Handle {
	if reg == nil do return nil
	if !reg.has_active do return nil
	return registry_get(reg, registry_active_stream_id(reg))
}

registry_get :: proc(reg: ^Stream_Registry, stream_id: string) -> ^Stream_Handle {
	if idx := registry_find(reg, stream_id); idx >= 0 do return &reg.handles[idx]
	return nil
}

registry_acquire :: proc(reg: ^Stream_Registry, stream_id: string, venue: string, symbol: string, market_type: string = "") -> ^Stream_Handle {
	h := registry_get_or_create(reg, stream_id, venue, symbol, market_type)
	if h == nil do return nil
	h.ref_count += 1
	h.paused = false
	return h
}

registry_release :: proc(reg: ^Stream_Registry, stream_id: string) -> bool {
	h := registry_get(reg, stream_id)
	if h == nil do return false
	if h.ref_count > 0 do h.ref_count -= 1
	if h.ref_count == 0 && reg.pause_when_unsubscribed {
		h.paused = true
	}
	return true
}

registry_reset_ref_counts :: proc(reg: ^Stream_Registry) {
	if reg == nil do return
	for i in 0 ..< len(reg.handles) {
		h := &reg.handles[i]
		if !h.used do continue
		h.ref_count = 0
		if reg.pause_when_unsubscribed {
			h.paused = true
		}
	}
}

registry_prune_unused :: proc(reg: ^Stream_Registry) {
	if reg == nil do return
	for i in 0 ..< len(reg.handles) {
		h := &reg.handles[i]
		if !h.used do continue
		if h.ref_count > 0 do continue
		if reg.has_active && registry_active_stream_id(reg) == stream_id(h) do continue
		h^ = {}
		if reg.count > 0 do reg.count -= 1
	}
}
