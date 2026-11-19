import type {Entry, Session} from './types';

type RequestOptions = {
  redirectOn401?: boolean;
};

type ApiInit = Omit<RequestInit, 'body'> & {
  body?: unknown;
};

export async function api<T>(
  path: string,
  init: ApiInit = {},
  options: RequestOptions = {},
): Promise<T> {
  const {body: inputBody, ...rest} = init;
  const headers = new Headers(init.headers);
  headers.set('X-Fileshare', '1');
  let body: BodyInit | null | undefined;
  if (inputBody !== undefined) {
    if (inputBody instanceof FormData) {
      body = inputBody;
    } else {
      headers.set('Content-Type', 'application/json');
      body = JSON.stringify(inputBody);
    }
  }
  const response = await fetch(path, {
    ...rest,
    headers,
    body,
    credentials: 'same-origin',
  });
  if (response.status === 401) {
    if (options.redirectOn401 !== false) window.location.replace('/login');
    throw new Error('Invalid password');
  }
  const contentType = response.headers.get('content-type') ?? '';
  const data: unknown = contentType.includes('application/json')
    ? await response.json().catch(() => null)
    : await response.text();
  if (!response.ok) {
    if (data && typeof data === 'object' && 'error' in data) {
      throw new Error(String((data as {error: unknown}).error));
    }
    if (typeof data === 'string' && data) throw new Error(data);
    throw new Error(`Request failed (${response.status})`);
  }
  return data as T;
}

export function getSession(): Promise<Session> {
  return api<Session>('/api/session');
}

export function login(password: string): Promise<{ok: boolean}> {
  return api<{ok: boolean}>(
    '/api/login',
    {method: 'POST', body: {password}},
    {redirectOn401: false},
  );
}

export function uploadFile(
  dir: string,
  file: File,
  onProgress: (percent: number) => void,
): Promise<Entry> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `/api/upload?${new URLSearchParams({path: dir}).toString()}`);
    xhr.setRequestHeader('X-Fileshare', '1');
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress((event.loaded / event.total) * 100);
    };
    xhr.onload = () => {
      if (xhr.status === 401) {
        window.location.replace('/login');
        reject(new Error('Unauthenticated'));
        return;
      }
      let data: unknown = null;
      try {
        data = JSON.parse(xhr.responseText);
      } catch {
        // ignore
      }
      if (xhr.status >= 200 && xhr.status < 300 && data && typeof data === 'object' && 'uploaded' in data) {
        const uploaded = (data as {uploaded: Entry[]}).uploaded;
        if (uploaded[0]) {
          resolve(uploaded[0]);
          return;
        }
      }
      if (data && typeof data === 'object' && 'error' in data) {
        reject(new Error(String((data as {error: unknown}).error)));
        return;
      }
      reject(new Error(`Upload failed (${xhr.status})`));
    };
    xhr.onerror = () => reject(new Error('Network error'));
    xhr.onabort = () => reject(new Error('Upload cancelled'));
    const form = new FormData();
    form.append('file', file, file.name);
    xhr.send(form);
  });
}
