import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { MultiSourcePicker } from './MultiSourcePicker';

function mount(over: Partial<Parameters<typeof MultiSourcePicker>[0]> = {}) {
  const onChange = vi.fn();
  render(
    <MultiSourcePicker
      pod="web-1"
      containers={['app', 'sidecar']}
      value={{ pod: 'web-1', container: 'app' }}
      onChange={onChange}
      {...over}
    />,
  );
  return onChange;
}

describe('MultiSourcePicker', () => {
  it('offers each container, all containers, a label selector and a workload', () => {
    mount();
    const options = screen.getAllByRole('option').map((o) => o.textContent);
    expect(options).toEqual([
      'app',
      'sidecar',
      'All containers',
      'Pods matching a label selector',
      'Pods of a workload',
    ]);
    expect(screen.getByLabelText('Source')).toHaveValue('container:app');
  });

  it('choosing a container follows that one container', async () => {
    const onChange = mount();
    await userEvent.selectOptions(screen.getByLabelText('Source'), 'container:sidecar');
    expect(onChange).toHaveBeenCalledWith({ pod: 'web-1', container: 'sidecar' });
  });

  it('choosing all containers follows every container of the pod', async () => {
    const onChange = mount();
    await userEvent.selectOptions(screen.getByLabelText('Source'), 'all');
    expect(onChange).toHaveBeenCalledWith({ pod: 'web-1', allContainers: true });
  });

  it('a label selector is applied with the button, not on every key', async () => {
    const onChange = mount({ value: { selector: 'app=web' } });
    expect(screen.getByLabelText('Source')).toHaveValue('selector');
    const input = screen.getByLabelText('Label selector');
    expect(input).toHaveValue('app=web');
    await userEvent.clear(input);
    await userEvent.type(input, 'app=api,tier=back');
    expect(onChange).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: 'Follow' }));
    expect(onChange).toHaveBeenCalledWith({ selector: 'app=api,tier=back' });
  });

  it('a selector needs text', async () => {
    mount({ value: { selector: '' } });
    expect(screen.getByRole('button', { name: 'Follow' })).toBeDisabled();
  });

  it('a workload is a kind and a name', async () => {
    const onChange = mount();
    await userEvent.selectOptions(screen.getByLabelText('Source'), 'workload');
    await userEvent.type(screen.getByLabelText('Workload name'), 'web');
    await userEvent.selectOptions(screen.getByLabelText('Workload kind'), 'StatefulSet');
    await userEvent.click(screen.getByRole('button', { name: 'Follow' }));
    expect(onChange).toHaveBeenCalledWith({ workload: { group: 'apps', kind: 'StatefulSet', name: 'web' } });
  });

  it('a Job is in the batch group', async () => {
    const onChange = mount({ value: { workload: { group: 'apps', kind: 'Deployment', name: 'x' } } });
    await userEvent.selectOptions(screen.getByLabelText('Workload kind'), 'Job');
    await userEvent.click(screen.getByRole('button', { name: 'Follow' }));
    expect(onChange).toHaveBeenCalledWith({ workload: { group: 'batch', kind: 'Job', name: 'x' } });
  });

  it('says how many containers at most are followed', () => {
    mount({ value: { selector: 'a=b' } });
    expect(screen.getByText(/at most 20 containers/i)).toBeInTheDocument();
  });
});
