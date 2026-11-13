import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState } from 'react';
import { BrowserRouter } from 'react-router';
import { ApiError, browserClient, type ApiClient } from './api/client';
import { ApiContext } from './api/context';
import { AppRoutes } from './features/shell/routes';

function newQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // 4xx answers are final; only network and server failures are worth another try.
        retry: (count, error) =>
          count < 2 && !(error instanceof ApiError && error.status < 500 && error.status > 0),
        refetchOnWindowFocus: false,
      },
    },
  });
}

export function App({ client }: { client?: ApiClient }) {
  const [queryClient] = useState(newQueryClient);
  const [defaultClient] = useState(() => browserClient());
  return (
    <QueryClientProvider client={queryClient}>
      <ApiContext.Provider value={client ?? defaultClient}>
        <BrowserRouter>
          <AppRoutes />
        </BrowserRouter>
      </ApiContext.Provider>
    </QueryClientProvider>
  );
}
