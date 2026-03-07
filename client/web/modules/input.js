// input.js — Keyboard + mouse input bridge, polled by WASM each frame.
//
// All state is encapsulated. Returns odin_env-compatible accessor functions
// and a hasPendingInput() predicate for idle throttling.

// Key enum ordinals: Up=0 Down=1 Left=2 Right=3 Enter=4 Escape=5 Tab=6 Space=7
//                    Num_1=8 Num_2=9 Num_3=10 Num_4=11 Num_5=12 Num_6=13
//                    Num_7=14 Num_8=15 Num_9=16 S=17 Slash=18 C=19 G=20 F=21
//                    M=22 B=23 V=24 R=25 I=26 H=27 J=28 K=29 Z=30 Delete=31

const KEY_MAP = {
    "ArrowUp": 0, "ArrowDown": 1, "ArrowLeft": 2, "ArrowRight": 3,
    "Enter": 4, "Escape": 5, "Tab": 6, " ": 7,
    "1": 8, "2": 9, "3": 10, "4": 11, "5": 12, "6": 13, "7": 14, "8": 15, "9": 16,
    "s": 17, "S": 17, "/": 18, "?": 18, "c": 19, "C": 19, "g": 20, "G": 20,
    "f": 21, "F": 21, "m": 22, "M": 22, "b": 23, "B": 23, "v": 24, "V": 24,
    "r": 25, "R": 25, "i": 26, "I": 26,
    "h": 27, "H": 27, "j": 28, "J": 28,
    "k": 29, "K": 29,
    "z": 30, "Z": 30,
    "Delete": 31, "Backspace": 31,
};

const MOUSE_WHEEL_SCALE = 1 / 100;

export function initInput(canvas) {
    let keyBits = 0;
    let keyPressedBits = 0;
    let keyReleasedBits = 0;
    let modifierBits = 0;
    let mouseX = 0;
    let mouseY = 0;
    let mouseButtons = 0;
    let mousePressedBits = 0;
    let mouseReleasedBits = 0;
    let mouseScrollX = 0;
    let mouseScrollY = 0;

    function updateModifiers(ev) {
        if (!ev) return;
        let bits = 0;
        if (ev.shiftKey) bits |= (1 << 0);
        if (ev.ctrlKey) bits |= (1 << 1);
        if (ev.altKey) bits |= (1 << 2);
        if (ev.metaKey) bits |= (1 << 3);
        modifierBits = bits;
    }

    function updateMousePos(ev) {
        if (!ev) return;
        if (!canvas) {
            mouseX = ev.clientX || 0;
            mouseY = ev.clientY || 0;
            return;
        }
        const rect = canvas.getBoundingClientRect();
        mouseX = (ev.clientX || 0) - rect.left;
        mouseY = (ev.clientY || 0) - rect.top;
    }

    // --- keyboard ---

    document.addEventListener("keydown", (ev) => {
        updateModifiers(ev);
        const bit = KEY_MAP[ev.key];
        if (bit !== undefined) {
            const mask = (1 << bit);
            if ((keyBits & mask) === 0) keyPressedBits |= mask;
            keyBits |= mask;
            if (ev.key === "Tab") ev.preventDefault();
        }
    }, { passive: false });

    document.addEventListener("keyup", (ev) => {
        updateModifiers(ev);
        const bit = KEY_MAP[ev.key];
        if (bit !== undefined) {
            const mask = (1 << bit);
            if ((keyBits & mask) !== 0) keyReleasedBits |= mask;
            keyBits &= ~mask;
        }
    });

    // --- mouse ---

    const target = canvas || document;

    target.addEventListener("mousemove", (ev) => {
        updateMousePos(ev);
        updateModifiers(ev);
    }, { passive: true });

    target.addEventListener("mousedown", (ev) => {
        updateMousePos(ev);
        updateModifiers(ev);
        if (ev.button === 0) {
            if ((mouseButtons & (1 << 0)) === 0) mousePressedBits |= (1 << 0);
            mouseButtons |= (1 << 0);
        } else if (ev.button === 2) {
            if ((mouseButtons & (1 << 1)) === 0) mousePressedBits |= (1 << 1);
            mouseButtons |= (1 << 1);
        } else if (ev.button === 1) {
            if ((mouseButtons & (1 << 2)) === 0) mousePressedBits |= (1 << 2);
            mouseButtons |= (1 << 2);
        }
    }, { passive: true });

    document.addEventListener("mouseup", (ev) => {
        updateMousePos(ev);
        updateModifiers(ev);
        if (ev.button === 0) {
            if ((mouseButtons & (1 << 0)) !== 0) mouseReleasedBits |= (1 << 0);
            mouseButtons &= ~(1 << 0);
        } else if (ev.button === 2) {
            if ((mouseButtons & (1 << 1)) !== 0) mouseReleasedBits |= (1 << 1);
            mouseButtons &= ~(1 << 1);
        } else if (ev.button === 1) {
            if ((mouseButtons & (1 << 2)) !== 0) mouseReleasedBits |= (1 << 2);
            mouseButtons &= ~(1 << 2);
        }
    }, { passive: true });

    target.addEventListener("wheel", (ev) => {
        updateMousePos(ev);
        updateModifiers(ev);
        mouseScrollX += -ev.deltaX * MOUSE_WHEEL_SCALE;
        mouseScrollY += -ev.deltaY * MOUSE_WHEEL_SCALE;
        ev.preventDefault();
    }, { passive: false });

    window.addEventListener("blur", () => {
        keyBits = 0;
        keyPressedBits = 0;
        keyReleasedBits = 0;
        modifierBits = 0;
        mouseButtons = 0;
        mousePressedBits = 0;
        mouseReleasedBits = 0;
        mouseScrollX = 0;
        mouseScrollY = 0;
    }, { passive: true });

    // --- public API (odin_env procs + idle check) ---

    return {
        hasPendingInput() {
            return keyPressedBits !== 0 ||
                keyReleasedBits !== 0 ||
                mousePressedBits !== 0 ||
                mouseReleasedBits !== 0 ||
                mouseScrollX !== 0 ||
                mouseScrollY !== 0;
        },

        key_state: () => keyBits,
        key_pressed_state() {
            const v = keyPressedBits >>> 0;
            keyPressedBits = 0;
            return v;
        },
        key_released_state() {
            const v = keyReleasedBits >>> 0;
            keyReleasedBits = 0;
            return v;
        },
        mouse_x: () => mouseX,
        mouse_y: () => mouseY,
        mouse_buttons: () => mouseButtons >>> 0,
        mouse_pressed_buttons() {
            const v = mousePressedBits >>> 0;
            mousePressedBits = 0;
            return v;
        },
        mouse_released_buttons() {
            const v = mouseReleasedBits >>> 0;
            mouseReleasedBits = 0;
            return v;
        },
        mouse_scroll_x() {
            const v = mouseScrollX;
            mouseScrollX = 0;
            return v;
        },
        mouse_scroll_y() {
            const v = mouseScrollY;
            mouseScrollY = 0;
            return v;
        },
        modifier_state: () => modifierBits >>> 0,
    };
}
