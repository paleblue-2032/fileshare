import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type ReactNode,
} from 'react';
import {AppShell} from '@astryxdesign/core/AppShell';
import {Banner} from '@astryxdesign/core/Banner';
import {Breadcrumbs, BreadcrumbItem} from '@astryxdesign/core/Breadcrumbs';
import {Button} from '@astryxdesign/core/Button';
import {Card} from '@astryxdesign/core/Card';
import {Center} from '@astryxdesign/core/Center';
import {DropdownMenu, type DropdownMenuOption} from '@astryxdesign/core/DropdownMenu';
import {EmptyState} from '@astryxdesign/core/EmptyState';
import {Icon} from '@astryxdesign/core/Icon';
import {IconButton} from '@astryxdesign/core/IconButton';
import {ProgressBar} from '@astryxdesign/core/ProgressBar';
import {Spinner} from '@astryxdesign/core/Spinner';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {useToast} from '@astryxdesign/core/Toast';
import {
  ArrowDown,
  ArrowUp,
  CloudUpload,
  Copy,
  Download,
  Eye,
  ExternalLink,
  FolderInput,
  FolderOpen,
  FolderPlus,
  HardDrive,
  Link2,
  LogOut,
  Menu,
  Pencil,
  RefreshCw,
  RotateCcw,
  Search,
  Share2,
  ShieldOff,
  Trash2,
  Upload,
  UserRound,
} from 'lucide-react';
import {api, getSession, uploadFile} from './api';
import type {
  Entry,
  Session,
  ShareItem,
  SortKey,
  SortState,
  TrashItem,
  UploadItem,
  View,
} from './types';
import {
  basename,
  copyText,
  fileUrl,
  formatBytes,
  formatDate,
  formatDateShort,
  kindFor,
  kindLabel,
  sortEntries,
  triggerDownload,
  zipUrl,
} from './utils';
import {Brand} from './components/Brand';
import {Row} from './components/FileRow';
import {UploadTray} from './components/UploadTray';
import {
  ConfirmDialog,
  InputDialog,
  MoveDialog,
  PreviewDialog,
  ShareDialog,
  type Notify,
} from './components/Dialogs';

type DialogState =
  | {type: 'input'; mode: 'new' | 'rename'; entry?: Entry}
  | {type: 'confirm'; title: string; message: string; confirmLabel: string; destructive?: boolean; onConfirm: () => Promise<void>}
  | {type: 'move'; entry: Entry}
  | {type: 'share'; entry: Entry}
  | {type: 'preview'; entry: Entry}
  | null;

type LoadOptions = {push?: boolean; replace?: boolean};

function urlForFiles(path: string): string {
  if (!path) return '/';
  return `/?${new URLSearchParams({p: path}).toString()}`;
}

function parseLocation(): {view: View; path: string} {
  const params = new URLSearchParams(window.location.search);
  const view = params.get('view');
  if (view === 'trash' || view === 'shares') return {view, path: ''};
  return {view: 'files', path: params.get('p') ?? ''};
}

function newUploadId(): string {
  return typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random()}`;
}

export function App(): ReactNode {
  const [session, setSession] = useState<Session | null>(null);
  const [view, setView] = useState<View>('files');
  const [filePath, setFilePath] = useState('');
  const [entries, setEntries] = useState<Entry[]>([]);
  const [searchResults, setSearchResults] = useState<Entry[] | null>(null);
  const [query, setQuery] = useState('');
  const [sort, setSort] = useState<SortState>({key: 'name', dir: 1});
  const [loading, setLoading] = useState(true);
  const [trash, setTrash] = useState<{items: TrashItem[]; totalBytes: number}>({
    items: [],
    totalBytes: 0,
  });
  const [shares, setShares] = useState<ShareItem[]>([]);
  const [uploads, setUploads] = useState<UploadItem[]>([]);
  const [dragging, setDragging] = useState(false);
  const [dialog, setDialog] = useState<DialogState>(null);

  const filePathRef = useRef('');
  const viewRef = useRef<View>('files');
  const uploadsRef = useRef<UploadItem[]>([]);
  const queueRef = useRef<string[]>([]);
  const activeUploadsRef = useRef(0);
  const uploadTimerRef = useRef<number | null>(null);
  const loadSeqRef = useRef(0);
  const dragDepthRef = useRef(0);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const showToast = useToast();

  const notify: Notify = useCallback(
    (message, type = 'info') => {
      showToast({
        body: message,
        type,
        isAutoHide: true,
        autoHideDuration: type === 'error' ? 7000 : 4000,
      });
    },
    [showToast],
  );

  useEffect(() => {
    filePathRef.current = filePath;
  }, [filePath]);

  useEffect(() => {
    viewRef.current = view;
  }, [view]);

  const refreshSession = useCallback(async () => {
    try {
      setSession(await getSession());
    } catch {
      // 401 already redirects.
    }
  }, []);

  const loadFiles = useCallback(
    async (path: string, options: LoadOptions = {}): Promise<void> => {
      const seq = ++loadSeqRef.current;
      setLoading(true);
      try {
        const data = await api<{path: string; entries: Entry[]}>(
          `/api/list?${new URLSearchParams({path}).toString()}`,
        );
        if (seq !== loadSeqRef.current) return;
        setView('files');
        setFilePath(data.path);
        setEntries(data.entries);
        setSearchResults(null);
        setQuery('');
        if (options.push) window.history.pushState({}, '', urlForFiles(data.path));
        else if (options.replace) window.history.replaceState({}, '', urlForFiles(data.path));
      } catch (cause) {
        if (seq !== loadSeqRef.current) return;
        if (path && cause instanceof Error && /not found/i.test(cause.message)) {
          setLoading(false);
          await loadFiles('', options);
          return;
        }
        notify(cause instanceof Error ? cause.message : 'Could not load this folder.', 'error');
      } finally {
        if (seq === loadSeqRef.current) setLoading(false);
      }
    },
    [notify],
  );

  const loadTrash = useCallback(
    async (options: LoadOptions = {}): Promise<void> => {
      const seq = ++loadSeqRef.current;
      setLoading(true);
      try {
        const data = await api<{items: TrashItem[]; totalBytes: number}>('/api/trash');
        if (seq !== loadSeqRef.current) return;
        setView('trash');
        setTrash(data);
        setSearchResults(null);
        setQuery('');
        if (options.push) window.history.pushState({}, '', '/?view=trash');
        else if (options.replace) window.history.replaceState({}, '', '/?view=trash');
      } catch (cause) {
        notify(cause instanceof Error ? cause.message : 'Could not load trash.', 'error');
      } finally {
        if (seq === loadSeqRef.current) setLoading(false);
      }
    },
    [notify],
  );

  const loadShares = useCallback(
    async (options: LoadOptions = {}): Promise<void> => {
      const seq = ++loadSeqRef.current;
      setLoading(true);
      try {
        const data = await api<{items: ShareItem[]}>('/api/shares');
        if (seq !== loadSeqRef.current) return;
        setView('shares');
        setShares(data.items);
        setSearchResults(null);
        setQuery('');
        if (options.push) window.history.pushState({}, '', '/?view=shares');
        else if (options.replace) window.history.replaceState({}, '', '/?view=shares');
      } catch (cause) {
        notify(cause instanceof Error ? cause.message : 'Could not load shared links.', 'error');
      } finally {
        if (seq === loadSeqRef.current) setLoading(false);
      }
    },
    [notify],
  );

  const loadFromLocation = useCallback(
    async (options: LoadOptions = {}): Promise<void> => {
      const location = parseLocation();
      if (location.view === 'trash') await loadTrash(options);
      else if (location.view === 'shares') await loadShares(options);
      else await loadFiles(location.path, options);
    },
    [loadFiles, loadTrash, loadShares],
  );

  useEffect(() => {
    let alive = true;
    getSession()
      .then(async (current) => {
        if (!alive) return;
        setSession(current);
        await loadFromLocation({replace: true});
      })
      .catch(() => {
        // 401 redirects to /login.
      });
    return () => {
      alive = false;
    };
  }, [loadFromLocation]);

  useEffect(() => {
    const onPopState = () => {
      void loadFromLocation();
    };
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, [loadFromLocation]);

  useEffect(() => {
    if (view !== 'files') return;
    const q = query.trim();
    if (!q) {
      setSearchResults(null);
      return;
    }
    let alive = true;
    const timer = window.setTimeout(() => {
      api<{entries: Entry[]}>(
        `/api/search?${new URLSearchParams({path: filePath, q}).toString()}`,
      )
        .then((data) => {
          if (alive) setSearchResults(data.entries);
        })
        .catch((cause) => {
          if (alive) notify(cause instanceof Error ? cause.message : 'Search failed.', 'error');
        });
    }, 250);
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
  }, [query, filePath, view, notify]);

  useEffect(() => {
    const title =
      view === 'files'
        ? filePath
          ? `${basename(filePath)} · Fileshare`
          : 'Fileshare'
        : view === 'trash'
          ? 'Trash · Fileshare'
          : 'Shared links · Fileshare';
    document.title = title;
  }, [view, filePath]);

  const visibleEntries = searchResults ?? entries;
  const sortedEntries = useMemo(
    () => sortEntries(visibleEntries, sort.key, sort.dir),
    [visibleEntries, sort],
  );

  const updateUpload = useCallback((id: string, patch: Partial<UploadItem>): void => {
    uploadsRef.current = uploadsRef.current.map((item) =>
      item.id === id ? {...item, ...patch} : item,
    );
    setUploads(uploadsRef.current);
  }, []);

  const scheduleFilesReload = useCallback(
    (dir: string): void => {
      if (viewRef.current !== 'files' || filePathRef.current !== dir) return;
      if (uploadTimerRef.current !== null) window.clearTimeout(uploadTimerRef.current);
      uploadTimerRef.current = window.setTimeout(() => {
        uploadTimerRef.current = null;
        if (viewRef.current === 'files' && filePathRef.current === dir) {
          void loadFiles(dir, {replace: true});
        }
      }, 350);
    },
    [loadFiles],
  );

  const pumpUploads = useCallback((): void => {
    while (activeUploadsRef.current < 2) {
      const id = queueRef.current.shift();
      if (!id) return;
      const item = uploadsRef.current.find((candidate) => candidate.id === id);
      if (!item) continue;
      activeUploadsRef.current += 1;
      updateUpload(id, {status: 'uploading', progress: 0});
      uploadFile(item.dir, item.file, (percent) => updateUpload(id, {progress: percent}))
        .then(() => {
          updateUpload(id, {status: 'done', progress: 100});
          void refreshSession();
          scheduleFilesReload(item.dir);
        })
        .catch((cause) => {
          updateUpload(id, {
            status: 'error',
            error: cause instanceof Error ? cause.message : 'Upload failed',
          });
          notify(cause instanceof Error ? cause.message : 'Upload failed', 'error');
        })
        .finally(() => {
          activeUploadsRef.current = Math.max(0, activeUploadsRef.current - 1);
          pumpUploads();
        });
    }
  }, [notify, refreshSession, scheduleFilesReload, updateUpload]);

  const enqueueFiles = useCallback(
    (files: FileList | File[]): void => {
      if (viewRef.current !== 'files') {
        notify('Switch back to Files before uploading.', 'error');
        return;
      }
      const max = session?.maxUploadBytes ?? 5 * 1024 * 1024 * 1024;
      const dir = filePathRef.current;
      const added: UploadItem[] = [];
      for (const file of Array.from(files)) {
        if (file.size > max) {
          notify(`${file.name} is larger than the ${formatBytes(max)} upload limit.`, 'error');
          continue;
        }
        added.push({
          id: newUploadId(),
          file,
          name: file.name,
          size: file.size,
          dir,
          progress: 0,
          status: 'queued',
        });
      }
      if (added.length === 0) return;
      uploadsRef.current = [...uploadsRef.current, ...added];
      setUploads(uploadsRef.current);
      queueRef.current.push(...added.map((item) => item.id));
      pumpUploads();
    },
    [notify, pumpUploads, session],
  );

  useEffect(() => {
    const hasFiles = (event: globalThis.DragEvent): boolean =>
      Array.from(event.dataTransfer?.types ?? []).includes('Files');
    const onDragEnter = (event: globalThis.DragEvent): void => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      dragDepthRef.current += 1;
      if (viewRef.current === 'files') setDragging(true);
    };
    const onDragOver = (event: globalThis.DragEvent): void => {
      if (hasFiles(event)) event.preventDefault();
    };
    const onDragLeave = (event: globalThis.DragEvent): void => {
      if (!hasFiles(event)) return;
      dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
      if (dragDepthRef.current === 0) setDragging(false);
    };
    const onDrop = (event: globalThis.DragEvent): void => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      dragDepthRef.current = 0;
      setDragging(false);
      if (viewRef.current === 'files' && event.dataTransfer?.files) {
        enqueueFiles(event.dataTransfer.files);
      }
    };
    document.addEventListener('dragenter', onDragEnter);
    document.addEventListener('dragover', onDragOver);
    document.addEventListener('dragleave', onDragLeave);
    document.addEventListener('drop', onDrop);
    return () => {
      document.removeEventListener('dragenter', onDragEnter);
      document.removeEventListener('dragover', onDragOver);
      document.removeEventListener('dragleave', onDragLeave);
      document.removeEventListener('drop', onDrop);
    };
  }, [enqueueFiles]);

  const clearFinishedUploads = useCallback((): void => {
    uploadsRef.current = uploadsRef.current.filter(
      (item) => item.status !== 'done' && item.status !== 'error',
    );
    setUploads(uploadsRef.current);
  }, []);

  const openEntry = useCallback(
    (entry: Entry): void => {
      if (entry.isDir) void loadFiles(entry.path, {push: true});
      else setDialog({type: 'preview', entry});
    },
    [loadFiles],
  );

  const reloadCurrent = useCallback(async (): Promise<void> => {
    if (viewRef.current === 'trash') await loadTrash();
    else if (viewRef.current === 'shares') await loadShares();
    else await loadFiles(filePathRef.current);
  }, [loadFiles, loadTrash, loadShares]);

  const openRename = useCallback((entry: Entry): void => {
    setDialog({type: 'input', mode: 'rename', entry});
  }, []);

  const openShare = useCallback((entry: Entry): void => {
    setDialog({type: 'share', entry});
  }, []);

  const openDelete = useCallback(
    (entry: Entry): void => {
      setDialog({
        type: 'confirm',
        title: 'Move to Trash?',
        message: `“${entry.name}” will be moved to Trash. You can restore it later.`,
        confirmLabel: 'Move to Trash',
        onConfirm: async () => {
          await api('/api/delete', {method: 'POST', body: {paths: [entry.path]}});
          notify('Moved to Trash', 'info');
          await refreshSession();
          await reloadCurrent();
        },
      });
    },
    [notify, refreshSession, reloadCurrent],
  );

  const openMove = useCallback((entry: Entry): void => {
    setDialog({type: 'move', entry});
  }, []);

  const fileMenu = useCallback(
    (entry: Entry): DropdownMenuOption[] => {
      const items: DropdownMenuOption[] = [];
      if (entry.isDir) {
        items.push({
          label: 'Open',
          icon: <Icon icon={FolderOpen} size="sm" />,
          onClick: () => void loadFiles(entry.path, {push: true}),
        });
        items.push({
          label: 'Download as ZIP',
          icon: <Icon icon={Download} size="sm" />,
          onClick: () => triggerDownload(zipUrl(entry.path)),
        });
      } else {
        items.push({
          label: 'Preview',
          icon: <Icon icon={Eye} size="sm" />,
          onClick: () => setDialog({type: 'preview', entry}),
        });
        items.push({
          label: 'Download',
          icon: <Icon icon={Download} size="sm" />,
          onClick: () => triggerDownload(fileUrl(entry.path)),
        });
      }
      items.push({
        label: 'Share link',
        icon: <Icon icon={Link2} size="sm" />,
        onClick: () => openShare(entry),
      });
      items.push({type: 'divider'});
      items.push({
        label: 'Rename',
        icon: <Icon icon={Pencil} size="sm" />,
        onClick: () => openRename(entry),
      });
      items.push({
        label: 'Move',
        icon: <Icon icon={FolderInput} size="sm" />,
        onClick: () => openMove(entry),
      });
      items.push({
        label: 'Move to Trash',
        icon: <Icon icon={Trash2} size="sm" />,
        variant: 'destructive',
        onClick: () => openDelete(entry),
      });
      return items;
    },
    [loadFiles, openDelete, openMove, openRename, openShare],
  );

  const restoreTrash = useCallback(
    async (item: TrashItem): Promise<void> => {
      try {
        await api('/api/trash/restore', {method: 'POST', body: {id: item.id}});
        notify('Restored', 'info');
        await refreshSession();
        await loadTrash();
      } catch (cause) {
        notify(cause instanceof Error ? cause.message : 'Could not restore.', 'error');
      }
    },
    [loadTrash, notify, refreshSession],
  );

  const purgeTrash = useCallback(
    (item: TrashItem): void => {
      setDialog({
        type: 'confirm',
        title: 'Delete forever?',
        message: `“${item.name}” will be permanently deleted. This cannot be undone.`,
        confirmLabel: 'Delete forever',
        onConfirm: async () => {
          await api('/api/trash/purge', {method: 'POST', body: {id: item.id}});
          notify('Deleted permanently', 'info');
          await refreshSession();
          await loadTrash();
        },
      });
    },
    [loadTrash, notify, refreshSession],
  );

  const emptyTrash = useCallback((): void => {
    setDialog({
      type: 'confirm',
      title: 'Empty Trash?',
      message: 'All items in Trash will be permanently deleted. This cannot be undone.',
      confirmLabel: 'Empty Trash',
      onConfirm: async () => {
        await api('/api/trash/empty', {method: 'POST', body: {}});
        notify('Trash emptied', 'info');
        await refreshSession();
        await loadTrash();
      },
    });
  }, [loadTrash, notify, refreshSession]);

  const trashMenu = useCallback(
    (item: TrashItem): DropdownMenuOption[] => [
      {
        label: 'Restore',
        icon: <Icon icon={RotateCcw} size="sm" />,
        onClick: () => void restoreTrash(item),
      },
      {
        label: 'Delete forever',
        icon: <Icon icon={Trash2} size="sm" />,
        variant: 'destructive',
        onClick: () => purgeTrash(item),
      },
    ],
    [purgeTrash, restoreTrash],
  );

  const revokeShare = useCallback(
    (item: ShareItem): void => {
      setDialog({
        type: 'confirm',
        title: 'Revoke link?',
        message: 'The link will stop working immediately.',
        confirmLabel: 'Revoke',
        onConfirm: async () => {
          await api(`/api/shares/${encodeURIComponent(item.token)}`, {method: 'DELETE'});
          notify('Link revoked', 'info');
          await loadShares();
        },
      });
    },
    [loadShares, notify],
  );

  const copyShare = useCallback(
    async (item: ShareItem): Promise<void> => {
      const copied = await copyText(item.url);
      notify(copied ? 'Copied to clipboard' : 'Could not copy the link.', copied ? 'info' : 'error');
    },
    [notify],
  );

  const shareMenu = useCallback(
    (item: ShareItem): DropdownMenuOption[] => [
      {
        label: 'Copy link',
        icon: <Icon icon={Copy} size="sm" />,
        onClick: () => void copyShare(item),
      },
      {
        label: 'Open link',
        icon: <Icon icon={ExternalLink} size="sm" />,
        onClick: () => window.open(item.url, '_blank', 'noopener'),
      },
      {
        label: 'Revoke',
        icon: <Icon icon={Trash2} size="sm" />,
        variant: 'destructive',
        onClick: () => revokeShare(item),
      },
    ],
    [copyShare, revokeShare],
  );

  const switchView = useCallback(
    (next: View): void => {
      if (next === viewRef.current) return;
      if (next === 'trash') void loadTrash({push: true});
      else if (next === 'shares') void loadShares({push: true});
      else void loadFiles(filePathRef.current, {push: true});
    },
    [loadFiles, loadShares, loadTrash],
  );

  const logout = useCallback(async (): Promise<void> => {
    try {
      await api('/api/logout', {method: 'POST', body: {}});
    } catch {
      // ignore
    }
    window.location.replace('/login');
  }, []);

  const logoutAll = useCallback(async (): Promise<void> => {
    try {
      await api('/api/sessions/revoke-all', {method: 'POST', body: {}});
    } catch {
      // ignore
    }
    window.location.replace('/login');
  }, []);

  if (!session) {
    return (
      <div className="fs-loading-page">
        <Center minHeight="100dvh">
          <Spinner label="Loading Fileshare" />
        </Center>
      </div>
    );
  }

  const insecure = window.location.protocol === 'http:';
  const used = session.usedBytes;
  const quota = session.quotaBytes;
  const usedPercent = quota > 0 ? (used / quota) * 100 : 0;
  const count = sortedEntries.length;
  const displayCount = view === 'files' ? count : view === 'trash' ? trash.items.length : shares.length;
  const policy = [
    session.retentionDays > 0
      ? `Files older than ${session.retentionDays} days are moved to Trash automatically.`
      : 'Automatic file cleanup is off.',
    session.trashDays > 0
      ? `Trash is emptied after ${session.trashDays} days.`
      : 'Trash is emptied manually.',
    session.webdav ? 'WebDAV is available at /dav/.' : '',
  ]
    .filter(Boolean)
    .join(' ');

  const breadcrumbs =
    view === 'files' ? (
      <Breadcrumbs
        label="Breadcrumb"
        variant="supporting"
        separator={<Icon icon="chevronRight" size="sm" />}
      >
        <BreadcrumbItem
          startIcon={<Icon icon={HardDrive} size="sm" />}
          onClick={() => void loadFiles('', {push: true})}
        >
          Files
        </BreadcrumbItem>
        {filePath
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
                        void loadFiles(prefix, {push: true});
                      }
                }
              >
                {part}
              </BreadcrumbItem>
            );
          })}
      </Breadcrumbs>
    ) : (
      <Breadcrumbs
        label="Breadcrumb"
        variant="supporting"
        separator={<Icon icon="chevronRight" size="sm" />}
      >
        <BreadcrumbItem
          startIcon={<Icon icon={HardDrive} size="sm" />}
          onClick={() => void loadFiles(filePathRef.current, {push: true})}
        >
          Files
        </BreadcrumbItem>
        <BreadcrumbItem isCurrent>{view === 'trash' ? 'Trash' : 'Shared links'}</BreadcrumbItem>
      </Breadcrumbs>
    );

  const sortControls =
    view === 'files' ? (
      <HStack gap={1} align="center" className="fs-sort">
        {(['name', 'size', 'mod'] as SortKey[]).map((key) => {
          const active = sort.key === key;
          const label = key === 'name' ? 'Name' : key === 'size' ? 'Size' : 'Modified';
          return (
            <Button
              key={key}
              label={label}
              size="sm"
              variant={active ? 'secondary' : 'ghost'}
              icon={active ? <Icon icon={sort.dir === 1 ? ArrowUp : ArrowDown} size="xsm" /> : undefined}
              onClick={() => {
                setSort((current) =>
                  current.key === key
                    ? {key, dir: current.dir === 1 ? -1 : 1}
                    : {key, dir: 1},
                );
              }}
            />
          );
        })}
      </HStack>
    ) : null;

  const viewInfo =
    view === 'files'
      ? searchResults !== null
        ? `${count} result${count === 1 ? '' : 's'} for “${query}”`
        : `${count} item${count === 1 ? '' : 's'}`
      : view === 'trash'
        ? `${trash.items.length} item${trash.items.length === 1 ? '' : 's'} · ${formatBytes(trash.totalBytes)}`
        : `${shares.length} link${shares.length === 1 ? '' : 's'}`;

  const emptyState =
    view === 'files' ? (
      searchResults !== null ? (
        <EmptyState
          title="No matches"
          description={`Nothing matched “${query}”.`}
          headingLevel={2}
          icon={<Icon icon={Search} size="lg" />}
          actions={
            <Button
              label="Clear search"
              variant="secondary"
              onClick={() => {
                setQuery('');
                setSearchResults(null);
              }}
            />
          }
        />
      ) : (
        <EmptyState
          title="This folder is empty"
          description="Upload files or create a folder to get started."
          headingLevel={2}
          icon={<Icon icon={FolderOpen} size="lg" />}
          actions={
            <HStack gap={2}>
              <Button
                label="Upload"
                variant="primary"
                icon={<Icon icon={Upload} size="sm" />}
                onClick={() => fileInputRef.current?.click()}
              />
              <Button
                label="New folder"
                variant="secondary"
                icon={<Icon icon={FolderPlus} size="sm" />}
                onClick={() => setDialog({type: 'input', mode: 'new'})}
              />
            </HStack>
          }
        />
      )
    ) : view === 'trash' ? (
      <EmptyState
        title="Trash is empty"
        description="Deleted files will appear here before they are permanently removed."
        headingLevel={2}
        icon={<Icon icon={Trash2} size="lg" />}
      />
    ) : (
      <EmptyState
        title="No shared links"
        description="Create a read-only link from any file or folder's menu."
        headingLevel={2}
        icon={<Icon icon={Link2} size="lg" />}
      />
    );

  const mobileMenuItems: DropdownMenuOption[] = [
    {
      label: 'New folder',
      icon: <Icon icon={FolderPlus} size="sm" />,
      isDisabled: view !== 'files',
      onClick: () => setDialog({type: 'input', mode: 'new'}),
    },
    {
      label: 'Trash',
      icon: <Icon icon={Trash2} size="sm" />,
      onClick: () => switchView('trash'),
    },
    {
      label: 'Shared links',
      icon: <Icon icon={Share2} size="sm" />,
      onClick: () => switchView('shares'),
    },
    {type: 'divider'},
    {
      label: 'Sign out',
      icon: <Icon icon={LogOut} size="sm" />,
      onClick: () => void logout(),
    },
    {
      label: 'Sign out everywhere',
      icon: <Icon icon={ShieldOff} size="sm" />,
      onClick: () => void logoutAll(),
    },
  ];

  const accountMenuItems: DropdownMenuOption[] = [
    {
      label: 'Sign out',
      icon: <Icon icon={LogOut} size="sm" />,
      onClick: () => void logout(),
    },
    {
      label: 'Sign out everywhere',
      icon: <Icon icon={ShieldOff} size="sm" />,
      onClick: () => void logoutAll(),
    },
  ];

  return (
    <>
      <AppShell
        height="fill"
        contentPadding={0}
        variant="surface"
        mobileNav={false}
        className="fs-shell"
        topNav={
          <nav className="fs-header" aria-label="Primary">
            <div className="fs-header-inner">
              <Brand />
              {view === 'files' && (
                <div className="fs-search">
                  <TextInput
                    label="Search"
                    isLabelHidden
                    type="search"
                    placeholder="Search in this folder"
                    value={query}
                    onChange={setQuery}
                    startIcon={Search}
                    hasClear
                  />
                </div>
              )}
              <HStack gap={2} align="center" className="fs-actions">
                <div className="fs-quota" title={`${formatBytes(used)} of ${formatBytes(quota)}`}>
                  <ProgressBar
                    label="Storage"
                    isLabelHidden
                    value={used}
                    max={quota}
                    variant={usedPercent >= 85 ? 'warning' : 'accent'}
                  />
                  <Text type="supporting" size="xsm">
                    {formatBytes(used)} / {formatBytes(quota)}
                  </Text>
                </div>
                <Button
                  label="Upload"
                  variant="primary"
                  icon={<Icon icon={Upload} size="sm" />}
                  onClick={() => fileInputRef.current?.click()}
                  isDisabled={view !== 'files'}
                />
                <IconButton
                  label="New folder"
                  tooltip="New folder"
                  variant="ghost"
                  className="fs-desktop-only"
                  icon={<Icon icon={FolderPlus} size="sm" />}
                  isDisabled={view !== 'files'}
                  onClick={() => setDialog({type: 'input', mode: 'new'})}
                />
                <IconButton
                  label="Trash"
                  tooltip="Trash"
                  variant={view === 'trash' ? 'secondary' : 'ghost'}
                  className="fs-desktop-only"
                  icon={<Icon icon={Trash2} size="sm" />}
                  onClick={() => switchView('trash')}
                />
                <IconButton
                  label="Shared links"
                  tooltip="Shared links"
                  variant={view === 'shares' ? 'secondary' : 'ghost'}
                  className="fs-desktop-only"
                  icon={<Icon icon={Share2} size="sm" />}
                  onClick={() => switchView('shares')}
                />
                <span className="fs-desktop-only">
                  <DropdownMenu
                    items={accountMenuItems}
                    renderTrigger={(props) => (
                      <IconButton
                        {...props}
                        label="Account"
                        tooltip="Account"
                        variant="ghost"
                        icon={<Icon icon={UserRound} size="sm" />}
                      />
                    )}
                  />
                </span>
                <span className="fs-mobile-only">
                  <DropdownMenu
                    items={mobileMenuItems}
                    renderTrigger={(props) => (
                      <IconButton
                        {...props}
                        label="More"
                        tooltip="More"
                        variant="ghost"
                        icon={<Icon icon={Menu} size="sm" />}
                      />
                    )}
                  />
                </span>
              </HStack>
            </div>
          </nav>
        }
      >
        <div className="fs-main">
          <VStack gap={4}>
            {insecure && (
              <Banner
                status="warning"
                title="Unencrypted connection"
                description="You are using HTTP. Use the Tailscale HTTPS URL so your password and files are encrypted."
              />
            )}
            <HStack justify="between" align="center" gap={3} className="fs-toolbar">
              <div className="fs-breadcrumbs">{breadcrumbs}</div>
              <HStack gap={2} align="center" className="fs-toolbar-end">
                {sortControls}
                <Text type="supporting" size="xsm" className="fs-view-info">
                  {viewInfo}
                </Text>
                {view === 'trash' && trash.items.length > 0 && (
                  <Button
                    label="Empty trash"
                    variant="destructive"
                    size="sm"
                    onClick={emptyTrash}
                  />
                )}
                <IconButton
                  label="Refresh"
                  tooltip="Refresh"
                  variant="ghost"
                  icon={<Icon icon={RefreshCw} size="sm" />}
                  onClick={() => void reloadCurrent()}
                />
              </HStack>
            </HStack>
            <Card padding={0} className="fs-list-card">
              <div className="fs-row-header">
                <Text type="supporting" size="xsm">
                  Name
                </Text>
                <Text type="supporting" size="xsm" className="fs-row-size">
                  Size
                </Text>
                <Text type="supporting" size="xsm" className="fs-row-date">
                  Modified
                </Text>
                <span />
              </div>
              {loading && displayCount === 0 ? (
                <Center padding={8}>
                  <Spinner label="Loading" />
                </Center>
              ) : displayCount === 0 ? (
                <div className="fs-empty">{emptyState}</div>
              ) : (
                <VStack gap={0}>
                  {view === 'files' &&
                    sortedEntries.map((entry) => (
                      <Row
                        key={entry.path}
                        kind={entry.isDir ? 'folder' : entry.kind}
                        name={entry.name}
                        description={
                          searchResults !== null ? (
                            entry.path
                          ) : (
                            <>
                              <span className="fs-row-kind">
                                {entry.isDir ? 'Folder' : kindLabel(entry.kind)}
                              </span>
                              <span className="fs-row-mobile-meta">
                                {entry.isDir
                                  ? formatDate(entry.modTime)
                                  : `${formatBytes(entry.size)} · ${formatDate(entry.modTime)}`}
                              </span>
                            </>
                          )
                        }
                        size={entry.isDir ? '—' : formatBytes(entry.size)}
                        date={formatDate(entry.modTime)}
                        mainLabel={`${entry.isDir ? 'Open folder' : 'Preview'} ${entry.name}`}
                        onMain={() => openEntry(entry)}
                        menuItems={fileMenu(entry)}
                      />
                    ))}
                  {view === 'trash' &&
                    trash.items.map((item) => (
                      <Row
                        key={item.id}
                        kind={item.isDir ? 'folder' : kindFor(item.name)}
                        name={item.name}
                        description={`${item.reason === 'retention' ? 'Expired · ' : ''}from /${item.originalPath}`}
                        size={formatBytes(item.size)}
                        date={formatDate(item.deletedAt)}
                        menuItems={trashMenu(item)}
                      />
                    ))}
                  {view === 'shares' &&
                    shares.map((item) => (
                      <Row
                        key={item.token}
                        kind={item.isDir ? 'folder' : kindFor(basename(item.path))}
                        name={basename(item.path)}
                        description={`/${item.path}`}
                        size="Read-only"
                        date={item.expiresAt ? `Expires ${formatDateShort(item.expiresAt)}` : 'No expiry'}
                        menuItems={shareMenu(item)}
                      />
                    ))}
                </VStack>
              )}
            </Card>
            <Text type="supporting" size="xsm" justify="center" className="fs-policy">
              {policy}
            </Text>
          </VStack>
        </div>
      </AppShell>

      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="fs-hidden-input"
        onChange={(event: ChangeEvent<HTMLInputElement>) => {
          if (event.currentTarget.files) enqueueFiles(event.currentTarget.files);
          event.currentTarget.value = '';
        }}
      />

      {dragging && (
        <div className="fs-drop-overlay">
          <Card padding={6} elevation="high">
            <VStack gap={2} align="center" hAlign="center">
              <Icon icon={CloudUpload} size="lg" />
              <Text type="body" weight="semibold">
                Drop files to upload
              </Text>
              <Text type="supporting">
                {filePath ? `Upload to /${filePath}` : 'Upload to Files'}
              </Text>
            </VStack>
          </Card>
        </div>
      )}

      <UploadTray uploads={uploads} onClear={clearFinishedUploads} />

      {dialog?.type === 'input' && dialog.mode === 'new' && (
        <InputDialog
          title="New folder"
          subtitle="Create a folder in the current location."
          label="Folder name"
          initialValue=""
          submitLabel="Create"
          onClose={() => setDialog(null)}
          notify={notify}
          onSubmit={async (name) => {
            await api('/api/mkdir', {
              method: 'POST',
              body: {path: filePathRef.current, name},
            });
            notify('Folder created', 'info');
            await loadFiles(filePathRef.current, {replace: true});
          }}
        />
      )}
      {dialog?.type === 'input' && dialog.mode === 'rename' && dialog.entry && (
        <InputDialog
          title={`Rename “${dialog.entry.name}”`}
          subtitle="Enter a new name for this item."
          label="Name"
          initialValue={dialog.entry.name}
          submitLabel="Rename"
          onClose={() => setDialog(null)}
          notify={notify}
          onSubmit={async (name) => {
            await api('/api/rename', {
              method: 'POST',
              body: {path: dialog.entry!.path, name},
            });
            notify('Renamed', 'info');
            await loadFiles(filePathRef.current, {replace: true});
          }}
        />
      )}
      {dialog?.type === 'confirm' && (
        <ConfirmDialog
          title={dialog.title}
          message={dialog.message}
          confirmLabel={dialog.confirmLabel}
          isDestructive={dialog.destructive !== false}
          onClose={() => setDialog(null)}
          notify={notify}
          onConfirm={dialog.onConfirm}
        />
      )}
      {dialog?.type === 'move' && (
        <MoveDialog
          source={dialog.entry}
          onClose={() => setDialog(null)}
          notify={notify}
          onMoved={async () => {
            await loadFiles(filePathRef.current, {replace: true});
          }}
        />
      )}
      {dialog?.type === 'share' && (
        <ShareDialog
          source={dialog.entry}
          onClose={() => setDialog(null)}
          notify={notify}
        />
      )}
      {dialog?.type === 'preview' && (
        <PreviewDialog entry={dialog.entry} onClose={() => setDialog(null)} />
      )}
    </>
  );
}
