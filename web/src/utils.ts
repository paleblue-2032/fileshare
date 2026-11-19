import type {SortKey, Entry} from './types';

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '—';
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB', 'PB'];
  let value = n / 1024;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[index]}`;
}

const dateFormat = new Intl.DateTimeFormat('en-US', {
  year: 'numeric',
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
});

const shortDateFormat = new Intl.DateTimeFormat('en-US', {
  year: 'numeric',
  month: 'short',
  day: 'numeric',
});

export function formatDate(value: string | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return dateFormat.format(date);
}

export function formatDateShort(value: string | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return shortDateFormat.format(date);
}

export function basename(path: string): string {
  const parts = path.split('/').filter(Boolean);
  return parts.length ? parts[parts.length - 1] : 'Files';
}

export function kindFor(name: string): string {
  const ext = (name.split('.').pop() ?? '').toLowerCase();
  if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'avif', 'bmp', 'heic'].includes(ext)) return 'image';
  if (['mp4', 'webm', 'mov', 'mkv', 'm4v'].includes(ext)) return 'video';
  if (['mp3', 'wav', 'ogg', 'flac', 'm4a', 'aac'].includes(ext)) return 'audio';
  if (ext === 'pdf') return 'pdf';
  if (['zip', 'tar', 'gz', 'tgz', 'bz2', 'xz', '7z', 'rar'].includes(ext)) return 'archive';
  if (
    [
      'txt', 'md', 'markdown', 'json', 'yaml', 'yml', 'toml', 'ini', 'conf', 'csv', 'log',
      'go', 'js', 'ts', 'tsx', 'jsx', 'css', 'html', 'htm', 'xml', 'sh', 'rs', 'c', 'h',
      'cpp', 'hpp', 'py', 'java', 'scala', 'nix',
    ].includes(ext)
  ) {
    return 'text';
  }
  return 'file';
}

export function kindLabel(kind: string): string {
  switch (kind) {
    case 'image':
      return 'Image';
    case 'video':
      return 'Video';
    case 'audio':
      return 'Audio';
    case 'pdf':
      return 'PDF document';
    case 'text':
      return 'Text file';
    case 'archive':
      return 'Archive';
    case 'folder':
      return 'Folder';
    default:
      return 'File';
  }
}

export function sortEntries(entries: Entry[], key: SortKey, dir: 1 | -1): Entry[] {
  return entries.slice().sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    let cmp = 0;
    if (key === 'size') cmp = a.size - b.size;
    else if (key === 'mod') cmp = new Date(a.modTime).getTime() - new Date(b.modTime).getTime();
    else cmp = a.name.localeCompare(b.name, undefined, {sensitivity: 'base', numeric: true});
    return cmp * dir;
  });
}

export function fileUrl(path: string, inline = false): string {
  const params = new URLSearchParams({path});
  if (inline) params.set('inline', '1');
  return `/api/download?${params.toString()}`;
}

export function zipUrl(path: string): string {
  return `/api/zip?${new URLSearchParams({path}).toString()}`;
}

export function triggerDownload(url: string): void {
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = '';
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const area = document.createElement('textarea');
    area.value = text;
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.append(area);
    area.select();
    try {
      return document.execCommand('copy');
    } finally {
      area.remove();
    }
  }
}
