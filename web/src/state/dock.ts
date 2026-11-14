import { useStore } from 'zustand';
import { createStore } from 'zustand/vanilla';

export interface LogTabProps {
  cluster: string;
  namespace: string;
  pod: string;
  containers: string[];
}

export interface ShellTabProps {
  cluster: string;
  namespace: string;
  pod: string;
  container: string;
  /** A node shell: the pod is removed when the tab closes. */
  nodeShell?: boolean;
}

export type DockTab =
  | { id: string; title: string; kind: 'logs'; props: LogTabProps }
  | { id: string; title: string; kind: 'shell'; props: ShellTabProps };

export interface DockState {
  tabs: DockTab[];
  active: string | null;
  /** Opens a tab, or activates the one with the same id. */
  open(tab: DockTab): void;
  close(id: string): void;
  activate(id: string): void;
}

export function createDockStore() {
  return createStore<DockState>()((set) => ({
    tabs: [],
    active: null,
    open: (tab) =>
      set((s) => ({
        tabs: s.tabs.some((t) => t.id === tab.id) ? s.tabs : [...s.tabs, tab],
        active: tab.id,
      })),
    close: (id) =>
      set((s) => {
        const i = s.tabs.findIndex((t) => t.id === id);
        if (i < 0) return s;
        const tabs = s.tabs.filter((t) => t.id !== id);
        const active = s.active === id ? (tabs[Math.min(i, tabs.length - 1)]?.id ?? null) : s.active;
        return { tabs, active };
      }),
    activate: (id) => set((s) => (s.tabs.some((t) => t.id === id) ? { active: id } : s)),
  }));
}

/** The dock of the running app: its tabs survive navigation because the state lives here. */
export const dockStore = createDockStore();

export function useDock(): DockState {
  return useStore(dockStore);
}

export function logTab(props: LogTabProps): DockTab {
  return {
    id: `logs:${props.cluster}/${props.namespace}/${props.pod}`,
    title: `Logs: ${props.pod}`,
    kind: 'logs',
    props,
  };
}

/** Every shell is its own tab: opening the same container twice gives two terminals. */
export function shellTab(props: ShellTabProps, serial: number): DockTab {
  return {
    id: `shell:${props.cluster}/${props.namespace}/${props.pod}/${props.container}/${serial}`,
    title: `Shell: ${props.pod}/${props.container}`,
    kind: 'shell',
    props,
  };
}
