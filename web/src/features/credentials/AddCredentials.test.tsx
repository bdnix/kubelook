import 'fake-indexeddb/auto';
import { screen } from '@testing-library/react';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it } from 'vitest';
import { renderApp } from '../../test/render';
import { AddCredentials } from './AddCredentials';
import { openVaultStore } from './vaultStore';

beforeEach(() => {
  globalThis.indexedDB = new IDBFactory();
});

describe('AddCredentials', () => {
  it('shows both forms when both are available', () => {
    renderApp(<AddCredentials features={{ vault: true, kubeconfig: true }} />, {
      vaultStore: openVaultStore('u1'),
    });
    expect(screen.getByLabelText('Vault token')).toBeInTheDocument();
    expect(screen.getByLabelText('Kubeconfig')).toBeInTheDocument();
  });

  it('hides the Vault form when Vault is not configured', () => {
    renderApp(<AddCredentials features={{ vault: false, kubeconfig: true }} />, {
      vaultStore: openVaultStore('u1'),
    });
    expect(screen.queryByLabelText('Vault token')).toBeNull();
    expect(screen.getByLabelText('Kubeconfig')).toBeInTheDocument();
  });

  it('hides the kubeconfig form when uploads are disabled', () => {
    renderApp(<AddCredentials features={{ vault: true, kubeconfig: false }} />, {
      vaultStore: openVaultStore('u1'),
    });
    expect(screen.getByLabelText('Vault token')).toBeInTheDocument();
    expect(screen.queryByLabelText('Kubeconfig')).toBeNull();
  });

  it('explains when neither is available', () => {
    renderApp(<AddCredentials features={{ vault: false, kubeconfig: false }} />, {
      vaultStore: openVaultStore('u1'),
    });
    expect(screen.getByText('This server accepts no credentials. Ask an administrator.')).toBeInTheDocument();
  });
});
