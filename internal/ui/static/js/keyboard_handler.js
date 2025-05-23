class KeyboardHandler {
    constructor() {
        this.queue = [];
        this.shortcuts = {};
        this.triggers = new Set();
    }

    on(combination, callback) {
        this.shortcuts[combination] = callback;
        this.triggers.add(combination.split(" ")[0]);
    }

    listen() {
        document.onkeydown = (event) => {
            const key = KeyboardHandler.getKey(event);
            if (this.isEventIgnored(event, key) || KeyboardHandler.isModifierKeyDown(event)) {
                return;
            }

            if (key !== "Enter") {
                event.preventDefault();
            }

            this.queue.push(key);

            for (const combination in this.shortcuts) {
                const keys = combination.split(" ");

                if (keys.every((value, index) => value === this.queue[index])) {
                    this.queue = [];
                    this.shortcuts[combination](event);
                    return;
                }

                if (keys.length === 1 && key === keys[0]) {
                    this.queue = [];
                    this.shortcuts[combination](event);
                    return;
                }
            }

            if (this.queue.length >= 2) {
                this.queue = [];
            }
        };
    }

    isEventIgnored(event, key) {
        return event.target.tagName === "INPUT" ||
            event.target.tagName === "TEXTAREA" ||
            (this.queue.length < 1 && !this.triggers.has(key));
    }

    static isModifierKeyDown(event) {
        return event.getModifierState("Control") || event.getModifierState("Alt") || event.getModifierState("Meta");
    }

    static getKey(event) {
        switch (event.key) {
        case 'Esc': return 'Escape';
        case 'Up': return 'ArrowUp';
        case 'Down': return 'ArrowDown';
        case 'Left': return 'ArrowLeft';
        case 'Right': return 'ArrowRight';
        default: return event.key;
        }
    }
}
