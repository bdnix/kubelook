import type { ReactNode } from 'react';
import { useModules } from './useModules';

/** Renders its children only when the module is active on the cluster; nothing while the modules load. */
export function ModuleGate({
  cluster,
  name,
  children,
  fallback = null,
}: {
  cluster: string;
  name: string;
  children: ReactNode;
  fallback?: ReactNode;
}) {
  const modules = useModules(cluster);
  if (!modules) return null;
  return modules.some((m) => m.name === name && m.active) ? <>{children}</> : <>{fallback}</>;
}
