export type Entry = {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modTime: string;
  kind: string;
};

export type TrashItem = {
  id: string;
  originalPath: string;
  name: string;
  isDir: boolean;
  size: number;
  deletedAt: string;
  reason: string;
};

export type ShareItem = {
  token: string;
  path: string;
  isDir: boolean;
  createdAt: string;
  expiresAt?: string;
  url: string;
};

export type Session = {
  authenticated: boolean;
  usedBytes: number;
  quotaBytes: number;
  trashBytes: number;
  retentionDays: number;
  trashDays: number;
  maxUploadBytes: number;
  webdav: boolean;
};

export type View = 'files' | 'trash' | 'shares';

export type SortKey = 'name' | 'size' | 'mod';

export type SortState = {
  key: SortKey;
  dir: 1 | -1;
};

export type UploadStatus = 'queued' | 'uploading' | 'done' | 'error';

export type UploadItem = {
  id: string;
  file: File;
  name: string;
  size: number;
  dir: string;
  progress: number;
  status: UploadStatus;
  error?: string;
};
