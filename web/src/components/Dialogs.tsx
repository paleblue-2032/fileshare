import {useEffect, useState, type FormEvent, type ReactNode} from 'react';
import {api} from '../api';
import {Banner} from '@astryxdesign/core/Banner';
import {Breadcrumbs, BreadcrumbItem} from '@astryxdesign/core/Breadcrumbs';
import {Button} from '@astryxdesign/core/Button';
import {Center} from '@astryxdesign/core/Center';
import {Dialog, DialogHeader} from '@astryxdesign/core/Dialog';
import {Icon} from '@astryxdesign/core/Icon';
import {List, ListItem} from '@astryxdesign/core/List';
import {Spinner} from '@astryxdesign/core/Spinner';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {Folder} from 'lucide-react';
import type {Entry} from '../types';
import {
  copyText,
  fileUrl,
  formatBytes,
  formatDate,
  triggerDownload,
} from '../utils';

export type Notify = (message: string, type?: 'info' | 'error') => void;

type CloseProps = {onClose: () => void; notify: Notify};

export function InputDialog({
  title,
  subtitle,
  label,
  initialValue,
  submitLabel,
  onClose,
  notify,
  onSubmit,
}: CloseProps & {
  title: string;
  subtitle?: string;
  label: string;
  initialValue: string;
  submitLabel: string;
  onSubmit: (value: string) => Promise<void>;
}): ReactNode {
  const [value, setValue] = useState(initialValue);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function doSubmit(): Promise<void> {
    const name = value.trim();
    if (!name) {
      setError('Enter a name.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await onSubmit(name);
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not save.');
      notify(cause instanceof Error ? cause.message : 'Could not save.', 'error');
      setBusy(false);
    }
  }

  function submit(event: FormEvent): void {
    event.preventDefault();
    void doSubmit();
  }

  return (
    <Dialog
      isOpen
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      width={460}
      purpose="form"
      padding={4}
    >
      <DialogHeader
        title={title}
        subtitle={subtitle}
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      />
      <form onSubmit={submit}>
        <VStack gap={3} paddingBlockStart={3}>
          <TextInput
            label={label}
            value={value}
            onChange={setValue}
            isRequired
            hasAutoFocus
            onEnter={() => {
              if (!busy) void doSubmit();
            }}
          />
          {error && <Banner status="error" title="Could not save" description={error} />}
          <HStack gap={2} justify="end">
            <Button label="Cancel" variant="ghost" onClick={onClose} />
            <Button label={submitLabel} variant="primary" type="submit" isLoading={busy} />
          </HStack>
        </VStack>
      </form>
    </Dialog>
  );
}

export function ConfirmDialog({
  title,
  message,
  confirmLabel,
  isDestructive = true,
  onClose,
  notify,
  onConfirm,
}: CloseProps & {
  title: string;
  message: string;
  confirmLabel: string;
  isDestructive?: boolean;
  onConfirm: () => Promise<void>;
}): ReactNode {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function confirm(): Promise<void> {
    setBusy(true);
    setError('');
    try {
      await onConfirm();
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'The operation failed.');
      notify(cause instanceof Error ? cause.message : 'The operation failed.', 'error');
      setBusy(false);
    }
  }

  return (
    <Dialog
      isOpen
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      width={440}
      purpose="info"
      padding={4}
    >
      <DialogHeader
        title={title}
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      />
      <VStack gap={3} paddingBlockStart={2}>
        <Text type="body">{message}</Text>
        {error && <Banner status="error" title="The operation failed" description={error} />}
        <HStack gap={2} justify="end">
          <Button label="Cancel" variant="ghost" onClick={onClose} />
          <Button
            label={confirmLabel}
            variant={isDestructive ? 'destructive' : 'primary'}
            onClick={() => void confirm()}
            isLoading={busy}
          />
        </HStack>
      </VStack>
    </Dialog>
  );
}

export function MoveDialog({
  source,
  onClose,
  notify,
  onMoved,
}: CloseProps & {
  source: Entry;
  onMoved: () => Promise<void>;
}): ReactNode {
  const [path, setPath] = useState('');
  const [entries, setEntries] = useState<Entry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    setError('');
    api<{entries: Entry[]}>(`/api/list?${new URLSearchParams({path}).toString()}`)
      .then((data) => {
        if (alive) setEntries(data.entries.filter((entry) => entry.isDir));
      })
      .catch((cause) => {
        if (alive) setError(cause instanceof Error ? cause.message : 'Could not load folders.');
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [path]);

  const blocked =
    source.isDir && (path === source.path || path.startsWith(`${source.path}/`));

  async function move(): Promise<void> {
    setBusy(true);
    setError('');
    try {
      await api('/api/move', {method: 'POST', body: {path: source.path, dest: path}});
      notify('Moved', 'info');
      await onMoved();
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not move.');
      setBusy(false);
    }
  }

  return (
    <Dialog
      isOpen
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      width={580}
      purpose="info"
      padding={4}
    >
      <DialogHeader
        title={`Move “${source.name}”`}
        subtitle="Choose a destination folder."
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      />
      <VStack gap={3} paddingBlockStart={3}>
        <Breadcrumbs
          label="Destination"
          variant="supporting"
          separator={<Icon icon="chevronRight" size="sm" />}
        >
          <BreadcrumbItem onClick={() => setPath('')}>Files</BreadcrumbItem>
          {path
            .split('/')
            .filter(Boolean)
            .map((part, index, parts) => {
              const prefix = parts.slice(0, index + 1).join('/');
              const isLast = index === parts.length - 1;
              return (
                <BreadcrumbItem
                  key={prefix}
                  isCurrent={isLast}
                  onClick={
                    isLast
                      ? undefined
                      : () => {
                          setPath(prefix);
                        }
                  }
                >
                  {part}
                </BreadcrumbItem>
              );
            })}
        </Breadcrumbs>
        {error && <Banner status="error" title="Could not load folders" description={error} />}
        {loading ? (
          <Center padding={4}>
            <Spinner label="Loading folders" />
          </Center>
        ) : entries.length === 0 ? (
          <Text type="supporting">No folders here. You can move into this location.</Text>
        ) : (
          <div className="fs-folder-picker">
            <List hasDividers density="compact">
              {entries.map((entry) => {
                const disabled =
                  source.isDir &&
                  (entry.path === source.path || entry.path.startsWith(`${source.path}/`));
                return (
                  <ListItem
                    key={entry.path}
                    label={entry.name}
                    startContent={<Icon icon={Folder} size="sm" />}
                    isDisabled={disabled}
                    onClick={disabled ? undefined : () => setPath(entry.path)}
                  />
                );
              })}
            </List>
          </div>
        )}
        <HStack gap={2} justify="end">
          <Button label="Cancel" variant="ghost" onClick={onClose} />
          <Button
            label="Move here"
            variant="primary"
            isDisabled={blocked}
            isLoading={busy}
            onClick={() => void move()}
          />
        </HStack>
      </VStack>
    </Dialog>
  );
}

const expiryOptions = [
  {value: '1', label: '1 hour'},
  {value: '24', label: '1 day'},
  {value: '168', label: '7 days'},
  {value: '720', label: '30 days'},
  {value: '0', label: 'Never'},
];

export function ShareDialog({
  source,
  onClose,
  notify,
}: CloseProps & {source: Entry}): ReactNode {
  const [expiry, setExpiry] = useState('168');
  const [url, setUrl] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function create(): Promise<void> {
    setBusy(true);
    setError('');
    try {
      const data = await api<{url: string}>('/api/shares', {
        method: 'POST',
        body: {path: source.path, expiresInHours: Number(expiry)},
      });
      setUrl(data.url);
      notify('Share link created', 'info');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not create a share link.');
    } finally {
      setBusy(false);
    }
  }

  async function copy(): Promise<void> {
    if (!url) return;
    const copied = await copyText(url);
    notify(copied ? 'Copied to clipboard' : 'Could not copy the link.', copied ? 'info' : 'error');
  }

  return (
    <Dialog
      isOpen
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      width={500}
      purpose="info"
      padding={4}
    >
      <DialogHeader
        title={`Share “${source.name}”`}
        subtitle="Anyone with the link can view and download this item."
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      />
      <VStack gap={3} paddingBlockStart={3}>
        {!url && (
          <div className="fs-field">
            <Text type="label" as="span">
              Link expires
            </Text>
            <select
              className="fs-select"
              aria-label="Link expires"
              value={expiry}
              onChange={(event) => setExpiry(event.target.value)}
            >
              {expiryOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>
        )}
        {error && <Banner status="error" title="Could not create a link" description={error} />}
        {url ? (
          <VStack gap={2}>
            <TextInput label="Share link" value={url} isReadOnly />
            <HStack gap={2}>
              <Button label="Copy link" variant="secondary" onClick={() => void copy()} />
              <Button label="Open" variant="ghost" onClick={() => window.open(url, '_blank', 'noopener')} />
            </HStack>
          </VStack>
        ) : (
          <HStack gap={2} justify="end">
            <Button label="Cancel" variant="ghost" onClick={onClose} />
            <Button label="Create link" variant="primary" onClick={() => void create()} isLoading={busy} />
          </HStack>
        )}
        {url && (
          <HStack justify="end">
            <Button label="Close" variant="ghost" onClick={onClose} />
          </HStack>
        )}
      </VStack>
    </Dialog>
  );
}

export function PreviewDialog({
  entry,
  onClose,
}: {
  entry: Entry;
  onClose: () => void;
}): ReactNode {
  const [text, setText] = useState('');
  const [textError, setTextError] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (entry.kind !== 'text' || entry.size > 1024 * 1024) return;
    const controller = new AbortController();
    setLoading(true);
    setText('');
    setTextError('');
    fetch(fileUrl(entry.path, true), {credentials: 'same-origin', signal: controller.signal})
      .then((response) => {
        if (!response.ok) throw new Error('Could not load preview.');
        return response.text();
      })
      .then(setText)
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        setTextError(cause instanceof Error ? cause.message : 'Could not load preview.');
      })
      .finally(() => setLoading(false));
    return () => controller.abort();
  }, [entry.kind, entry.path, entry.size]);

  return (
    <Dialog
      isOpen
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      width={980}
      maxHeight="92dvh"
      purpose="info"
      padding={3}
    >
      <DialogHeader
        title={entry.name}
        subtitle={`${formatBytes(entry.size)} · ${formatDate(entry.modTime)}`}
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      />
      <div className="fs-preview">
        {entry.kind === 'image' && (
          <img src={fileUrl(entry.path, true)} alt={entry.name} className="fs-preview-media" />
        )}
        {entry.kind === 'video' && (
          <video src={fileUrl(entry.path, true)} controls className="fs-preview-media" />
        )}
        {entry.kind === 'audio' && (
          <audio src={fileUrl(entry.path, true)} controls className="fs-preview-audio" />
        )}
        {entry.kind === 'pdf' && (
          <iframe src={fileUrl(entry.path, true)} title={entry.name} className="fs-preview-frame" />
        )}
        {entry.kind === 'text' &&
          (entry.size > 1024 * 1024 ? (
            <Text type="supporting">This file is too large to preview. Download it to view it.</Text>
          ) : loading ? (
            <Center padding={6}>
              <Spinner label="Loading preview" />
            </Center>
          ) : textError ? (
            <Banner status="error" title="Could not load preview" description={textError} />
          ) : (
            <pre className="fs-preview-text">{text}</pre>
          ))}
        {!['image', 'video', 'audio', 'pdf', 'text'].includes(entry.kind) && (
          <Text type="supporting">No preview available for this file type. Download it to open it.</Text>
        )}
      </div>
      <HStack gap={2} justify="end" paddingBlockStart={3}>
        <Button
          label="Download"
          variant="primary"
          onClick={() => triggerDownload(fileUrl(entry.path))}
        />
        <Button label="Close" variant="ghost" onClick={onClose} />
      </HStack>
    </Dialog>
  );
}
