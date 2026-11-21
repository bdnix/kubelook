import { useEffect, useState } from 'react';
import { useSocket } from '../../ws/context';
import type { MapNode } from './legend';

export interface MapEdge {
  from: string;
  to: string;
  type: string;
}

export interface MapGraph {
  nodes: MapNode[];
  edges: MapEdge[];
  /** Kinds the user may not list: the map is drawn without them. */
  omittedKinds: string[];
  /** Selected namespaces a map of this size could not watch. */
  leftOutNamespaces?: string[];
}

export interface MapFocus {
  group: string;
  kind: string;
  namespace: string;
  name: string;
}

export interface MapTarget {
  cluster: string;
  /** Empty means all namespaces. */
  namespaces: string[];
  focus?: MapFocus;
}

export type MapStatus = 'idle' | 'loading' | 'ready' | 'error';

/** The live graph of a map channel. A new target closes the old channel and opens another. */
export function useResourceMap(target: MapTarget | undefined): {
  status: MapStatus;
  graph?: MapGraph;
  error?: string;
} {
  const socket = useSocket();
  const [state, setState] = useState<{ status: MapStatus; graph?: MapGraph; error?: string }>({
    status: 'idle',
  });
  const signature = JSON.stringify(target ?? null);
  useEffect(() => {
    if (!target) {
      setState({ status: 'idle' });
      return undefined;
    }
    setState({ status: 'loading' });
    const channel = socket.open(
      'map.open',
      {
        cluster: target.cluster,
        namespaces: target.namespaces,
        ...(target.focus && { focus: target.focus }),
      },
      {
        onFrame(type, data) {
          if (type === 'graph') setState({ status: 'ready', graph: data as MapGraph });
          else if (type === 'error')
            setState({
              status: 'error',
              error: (data as { message?: string })?.message ?? 'The map could not be drawn.',
            });
        },
      },
    );
    return () => channel.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the target is compared by its JSON
  }, [socket, signature]);
  return state;
}
