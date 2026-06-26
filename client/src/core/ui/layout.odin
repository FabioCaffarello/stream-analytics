package ui

// Layout primitives (pure). Rect operations, alignment, splitting.

import "core:math"

// --- Alignment ---

Align_H :: enum {
	Left,
	Center,
	Right,
}

Align_V :: enum {
	Top,
	Middle,
	Bottom,
}

// --- Rect constructors ---

rect_xywh :: proc(x, y, w, h: f32) -> Rect {
	return {pos = {x, y}, size = {w, h}}
}

// --- Rect queries ---

rect_right :: proc(r: Rect) -> f32 {
	return r.pos.x + r.size.x
}

rect_bottom :: proc(r: Rect) -> f32 {
	return r.pos.y + r.size.y
}

rect_contains :: proc(r: Rect, p: Vec2) -> bool {
	return p.x >= r.pos.x && p.x < rect_right(r) &&
	       p.y >= r.pos.y && p.y < rect_bottom(r)
}

// --- Rect transforms ---

rect_pad :: proc(r: Rect, p: f32) -> Rect {
	return {
		pos  = {r.pos.x + p, r.pos.y + p},
		size = {math.max(r.size.x - 2 * p, 0), math.max(r.size.y - 2 * p, 0)},
	}
}

rect_pad_xy :: proc(r: Rect, h, v: f32) -> Rect {
	return {
		pos  = {r.pos.x + h, r.pos.y + v},
		size = {math.max(r.size.x - 2 * h, 0), math.max(r.size.y - 2 * v, 0)},
	}
}

// --- Splitting (returns two halves) ---

rect_split_h :: proc(r: Rect, ratio: f32) -> (left: Rect, right: Rect) {
	w := r.size.x * clamp(ratio, 0, 1)
	left  = {pos = r.pos, size = {w, r.size.y}}
	right = {pos = {r.pos.x + w, r.pos.y}, size = {r.size.x - w, r.size.y}}
	return
}

rect_split_v :: proc(r: Rect, ratio: f32) -> (top: Rect, bottom: Rect) {
	h := r.size.y * clamp(ratio, 0, 1)
	top    = {pos = r.pos, size = {r.size.x, h}}
	bottom = {pos = {r.pos.x, r.pos.y + h}, size = {r.size.x, r.size.y - h}}
	return
}

// --- Cutting (mutates source, returns cut piece) ---

rect_cut_top :: proc(r: ^Rect, amount: f32) -> Rect {
	a := math.min(amount, r.size.y)
	cut := Rect{pos = r.pos, size = {r.size.x, a}}
	r.pos.y  += a
	r.size.y -= a
	return cut
}

rect_cut_bottom :: proc(r: ^Rect, amount: f32) -> Rect {
	a := math.min(amount, r.size.y)
	r.size.y -= a
	return Rect{pos = {r.pos.x, r.pos.y + r.size.y}, size = {r.size.x, a}}
}

rect_cut_left :: proc(r: ^Rect, amount: f32) -> Rect {
	a := math.min(amount, r.size.x)
	cut := Rect{pos = r.pos, size = {a, r.size.y}}
	r.pos.x  += a
	r.size.x -= a
	return cut
}

rect_cut_right :: proc(r: ^Rect, amount: f32) -> Rect {
	a := math.min(amount, r.size.x)
	r.size.x -= a
	return Rect{pos = {r.pos.x + r.size.x, r.pos.y}, size = {a, r.size.y}}
}

// --- Alignment within a rect ---

align_in_rect :: proc(r: Rect, item_size: Vec2, h: Align_H, v: Align_V) -> Vec2 {
	x: f32
	switch h {
	case .Left:   x = r.pos.x
	case .Center: x = r.pos.x + (r.size.x - item_size.x) * 0.5
	case .Right:  x = r.pos.x + r.size.x - item_size.x
	}
	y: f32
	switch v {
	case .Top:    y = r.pos.y
	case .Middle: y = r.pos.y + (r.size.y - item_size.y) * 0.5
	case .Bottom: y = r.pos.y + r.size.y - item_size.y
	}
	return {x, y}
}
