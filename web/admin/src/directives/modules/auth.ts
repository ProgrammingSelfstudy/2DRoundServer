import { shallowRef, watchEffect, type Directive } from "vue";
import { useAuthStore } from "@/stores/modules/auth";
interface Control {
  value: ReturnType<typeof shallowRef>;
  stop: () => void;
  display: string;
}
const controls = new WeakMap<HTMLElement, Control>();
const auth: Directive<HTMLElement> = {
  mounted(el, binding) {
    const store = useAuthStore();
    const value = shallowRef(binding.value);
    const display = el.style.display;
    const stop = watchEffect(() => {
      const permissions = store.authButtonListGet[store.routeName] || [];
      const required = Array.isArray(value.value) ? value.value : [value.value];
      const allowed = required.length > 0 && required.every(item => typeof item === "string" && permissions.includes(item));
      el.style.display = allowed ? display : "none";
    });
    controls.set(el, { value, stop, display });
  },
  updated(el, binding) {
    const state = controls.get(el);
    if (state) state.value.value = binding.value;
  },
  unmounted(el) {
    controls.get(el)?.stop();
    controls.delete(el);
  }
};
export default auth;
