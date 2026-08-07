const elements = {
  authDialog: document.querySelector('#authDialog'),
  authForm: document.querySelector('#authForm'),
  codeInput: document.querySelector('#codeInput'),
  codeError: document.querySelector('#codeError'),
  connectButton: document.querySelector('#connectButton'),
  dropZone: document.querySelector('#dropZone'),
  fileInput: document.querySelector('#fileInput'),
  chooseButton: document.querySelector('#chooseButton'),
  queueSection: document.querySelector('#queueSection'),
  uploadQueue: document.querySelector('#uploadQueue'),
  queueTemplate: document.querySelector('#queueTemplate'),
  fileTemplate: document.querySelector('#fileTemplate'),
  fileList: document.querySelector('#fileList'),
  loadingState: document.querySelector('#loadingState'),
  emptyState: document.querySelector('#emptyState'),
  refreshButton: document.querySelector('#refreshButton'),
  fileCount: document.querySelector('#fileCount'),
  totalSize: document.querySelector('#totalSize'),
  maxSize: document.querySelector('#maxSize'),
  toast: document.querySelector('#toast'),
  connectionPill: document.querySelector('#connectionPill'),
  connectionLabel: document.querySelector('#connectionLabel'),
  shareButton: document.querySelector('#shareButton'),
  shareDialog: document.querySelector('#shareDialog'),
  shareQR: document.querySelector('#shareQR'),
  shareAddress: document.querySelector('#shareAddress'),
  copyShareButton: document.querySelector('#copyShareButton'),
};

let uploadChain = Promise.resolve();
let toastTimer;
let maxBytes = Number.POSITIVE_INFINITY;
let shareURL = '';

class AuthRequiredError extends Error {}

async function createSession(token) {
  const response = await fetch('/api/session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.message || '连接失败');
  }
}

async function apiFetch(path, options = {}) {
  const response = await fetch(path, options);
  if (response.status === 401) {
    showAuthDialog();
    throw new AuthRequiredError('需要连接码');
  }
  return response;
}

function showAuthDialog(message = '') {
  setConnectionState('locked');
  elements.codeError.hidden = !message;
  elements.codeError.textContent = message;
  if (!elements.authDialog.open) {
    elements.authDialog.showModal();
  }
  requestAnimationFrame(() => elements.codeInput.focus());
}

async function loadFiles() {
  elements.refreshButton.classList.add('is-loading');
  try {
    const response = await apiFetch('/api/files');
    if (!response.ok) {
      throw new Error('无法读取文件列表');
    }
    const data = await response.json();
    setConnectionState('connected');
    maxBytes = data.maxBytes;
    renderStats(data);
    renderFiles(data.files);
  } catch (error) {
    if (!(error instanceof AuthRequiredError)) {
      showToast(error.message || '加载失败');
    }
  } finally {
    elements.loadingState.hidden = true;
    elements.refreshButton.classList.remove('is-loading');
  }
}

function renderStats(data) {
  elements.fileCount.textContent = new Intl.NumberFormat('zh-CN').format(data.count);
  elements.totalSize.textContent = formatSize(data.totalSize);
  elements.maxSize.textContent = formatSize(data.maxBytes);
}

function renderFiles(files) {
  elements.fileList.replaceChildren();
  elements.emptyState.hidden = files.length !== 0;

  for (const file of files) {
    const row = elements.fileTemplate.content.firstElementChild.cloneNode(true);
    row.querySelector('.file-name').textContent = file.name;
    row.querySelector('.file-meta').textContent = `${formatSize(file.size)} · ${formatDate(file.modified)}`;
    row.querySelector('.file-hash').textContent = `SHA-256  ${shortHash(file.sha256)}`;

    const download = row.querySelector('.download-button');
    download.href = `/api/files/${encodeURIComponent(file.name)}`;
    download.download = file.name;
    download.setAttribute('aria-label', `下载 ${file.name}`);

    const deleteButton = row.querySelector('.delete-button');
    deleteButton.setAttribute('aria-label', `删除 ${file.name}`);
    deleteButton.addEventListener('click', () => deleteFile(file.name, row));
    elements.fileList.append(row);
  }
}

async function deleteFile(name, row) {
  if (!window.confirm(`确定删除“${name}”吗？此操作无法撤销。`)) {
    return;
  }

  const button = row.querySelector('.delete-button');
  button.disabled = true;
  try {
    const response = await apiFetch(`/api/files/${encodeURIComponent(name)}`, { method: 'DELETE' });
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new Error(body.message || '删除失败');
    }
    row.remove();
    showToast(`已删除 ${name}`);
    await loadFiles();
  } catch (error) {
    if (!(error instanceof AuthRequiredError)) {
      showToast(error.message || '删除失败');
    }
    button.disabled = false;
  }
}

function enqueueFiles(fileList) {
  const files = Array.from(fileList);
  if (files.length === 0) return;

  elements.queueSection.hidden = false;
  for (const file of files) {
    const task = createQueueTask(file);
    uploadChain = uploadChain.then(() => uploadFile(task));
  }
}

function createQueueTask(file) {
  const row = elements.queueTemplate.content.firstElementChild.cloneNode(true);
  row.querySelector('.queue-name').textContent = file.name;
  row.querySelector('.queue-size').textContent = formatSize(file.size);
  elements.uploadQueue.prepend(row);

  const task = {
    file,
    row,
    xhr: null,
    cancelled: false,
    startedAt: 0,
    lastSampleAt: 0,
    lastLoaded: 0,
    speed: 0,
  };
  row.querySelector('.cancel-button').addEventListener('click', () => {
    task.cancelled = true;
    if (task.xhr) task.xhr.abort();
    setTaskState(task, 'error', '已取消', 0);
  });
  return task;
}

function uploadFile(task) {
  return new Promise((resolve) => {
    if (task.cancelled) {
      resolve();
      return;
    }
    if (task.file.size > maxBytes) {
      setTaskState(task, 'error', `超过 ${formatSize(maxBytes)} 上限`, 0);
      resolve();
      return;
    }

    const xhr = new XMLHttpRequest();
    task.xhr = xhr;
    xhr.open('POST', '/api/files');
    xhr.responseType = 'json';

    xhr.upload.addEventListener('progress', (event) => {
      if (!event.lengthComputable) return;
      const percent = Math.min(100, Math.round((event.loaded / event.total) * 100));
      setTaskState(task, '', `${percent}%`, percent);
      updateTransferMetrics(task, event.loaded, event.total);
    });

    xhr.addEventListener('load', async () => {
      if (xhr.status === 201) {
        setTaskState(task, 'success', '已完成', 100);
        finishTransferMetrics(task);
        showToast(`${task.file.name} 上传完成`);
        await loadFiles();
      } else if (xhr.status === 401) {
        setTaskState(task, 'error', '需要重新连接', 0);
        showAuthDialog();
      } else {
        setTaskState(task, 'error', xhr.response?.message || '上传失败', 0);
      }
      resolve();
    });

    xhr.addEventListener('error', () => {
      setTaskState(task, 'error', '网络连接中断', 0);
      resolve();
    });

    xhr.addEventListener('abort', () => resolve());

    const form = new FormData();
    form.append('file', task.file, task.file.name);
    task.startedAt = performance.now();
    task.lastSampleAt = task.startedAt;
    setTaskState(task, '', '正在上传', 0);
    xhr.send(form);
  });
}

function updateTransferMetrics(task, loaded, total) {
  const now = performance.now();
  const elapsed = Math.max((now - task.startedAt) / 1000, 0.001);
  const sampleSeconds = (now - task.lastSampleAt) / 1000;

  if (sampleSeconds >= 0.18 || loaded >= total) {
    const instantaneous = Math.max(0, loaded - task.lastLoaded) / Math.max(sampleSeconds, 0.001);
    task.speed = task.speed === 0 ? instantaneous : task.speed * 0.68 + instantaneous * 0.32;
    task.lastSampleAt = now;
    task.lastLoaded = loaded;
  }

  const average = loaded / elapsed;
  const displaySpeed = task.speed || average;
  task.row.querySelector('.queue-speed').textContent = elapsed < 0.12 ? '计算速度…' : formatSpeed(displaySpeed);
  task.row.querySelector('.queue-eta').textContent = displaySpeed > 0 ? `剩余 ${formatDuration((total - loaded) / displaySpeed)}` : '';
}

function finishTransferMetrics(task) {
  const elapsed = Math.max((performance.now() - task.startedAt) / 1000, 0.001);
  const average = task.file.size / elapsed;
  task.row.querySelector('.queue-speed').textContent = `平均 ${formatSpeed(average)}`;
  task.row.querySelector('.queue-eta').textContent = `用时 ${formatDuration(elapsed)}`;
}

function setTaskState(task, state, label, percent) {
  task.row.classList.toggle('is-success', state === 'success');
  task.row.classList.toggle('is-error', state === 'error');
  task.row.querySelector('.queue-status').textContent = label;
  const progress = task.row.querySelector('.progress-track');
  progress.setAttribute('aria-valuenow', String(percent));
  task.row.querySelector('.progress-bar').style.width = `${percent}%`;
  if (state) {
    task.row.querySelector('.cancel-button').hidden = true;
  }
}

function formatSize(bytes) {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / (1024 ** index);
  const digits = value >= 100 || index === 0 ? 0 : value >= 10 ? 1 : 2;
  return `${value.toFixed(digits)} ${units[index]}`;
}

function formatDate(value) {
  const date = new Date(value);
  return new Intl.DateTimeFormat('zh-CN', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

function formatSpeed(bytesPerSecond) {
  return `${formatSize(bytesPerSecond)}/s`;
}

function formatDuration(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds < 1) return '< 1 秒';
  if (seconds < 60) return `${Math.ceil(seconds)} 秒`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = Math.ceil(seconds % 60);
  return remainingSeconds === 0 ? `${minutes} 分钟` : `${minutes} 分 ${remainingSeconds} 秒`;
}

function shortHash(hash) {
  if (!hash) return '计算中';
  return `${hash.slice(0, 12)}…${hash.slice(-8)}`;
}

function showToast(message) {
  clearTimeout(toastTimer);
  elements.toast.textContent = message;
  elements.toast.hidden = false;
  toastTimer = setTimeout(() => {
    elements.toast.hidden = true;
  }, 3200);
}

function setConnectionState(state) {
  elements.connectionPill.classList.toggle('is-waiting', state === 'waiting');
  elements.connectionPill.classList.toggle('is-locked', state === 'locked');
  elements.connectionLabel.textContent = state === 'connected' ? '已连接' : state === 'locked' ? '需要连接码' : '正在验证';
}

async function openShareDialog() {
  elements.shareButton.disabled = true;
  try {
    const response = await apiFetch('/api/share');
    if (!response.ok) throw new Error('无法生成连接二维码');
    const data = await response.json();
    shareURL = data.url;
    elements.shareAddress.textContent = data.address;
    elements.shareQR.src = `/api/share/qr?v=${Date.now()}`;
    if (!elements.shareDialog.open) elements.shareDialog.showModal();
  } catch (error) {
    if (!(error instanceof AuthRequiredError)) showToast(error.message || '二维码生成失败');
  } finally {
    elements.shareButton.disabled = false;
  }
}

async function copyShareLink() {
  if (!shareURL) return;
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(shareURL);
    } else {
      const input = document.createElement('textarea');
      input.value = shareURL;
      input.setAttribute('readonly', '');
      input.style.position = 'fixed';
      input.style.opacity = '0';
      document.body.append(input);
      input.select();
      document.execCommand('copy');
      input.remove();
    }
    showToast('连接链接已复制');
  } catch {
    showToast('复制失败，请使用二维码连接');
  }
}

elements.chooseButton.addEventListener('click', (event) => {
  event.stopPropagation();
  elements.fileInput.click();
});
elements.dropZone.addEventListener('click', () => elements.fileInput.click());
elements.dropZone.addEventListener('keydown', (event) => {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    elements.fileInput.click();
  }
});
elements.fileInput.addEventListener('change', () => {
  enqueueFiles(elements.fileInput.files);
  elements.fileInput.value = '';
});

let dragDepth = 0;
elements.dropZone.addEventListener('dragenter', (event) => {
  event.preventDefault();
  dragDepth += 1;
  elements.dropZone.classList.add('is-dragging');
});
elements.dropZone.addEventListener('dragover', (event) => {
  event.preventDefault();
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy';
});
elements.dropZone.addEventListener('dragleave', () => {
  dragDepth = Math.max(0, dragDepth - 1);
  if (dragDepth === 0) elements.dropZone.classList.remove('is-dragging');
});
elements.dropZone.addEventListener('drop', (event) => {
  event.preventDefault();
  dragDepth = 0;
  elements.dropZone.classList.remove('is-dragging');
  enqueueFiles(event.dataTransfer?.files || []);
});

elements.refreshButton.addEventListener('click', loadFiles);
elements.shareButton.addEventListener('click', openShareDialog);
elements.copyShareButton.addEventListener('click', copyShareLink);
elements.authDialog.addEventListener('cancel', (event) => event.preventDefault());
elements.authForm.addEventListener('submit', async (event) => {
  event.preventDefault();
  const token = elements.codeInput.value.trim();
  elements.connectButton.disabled = true;
  elements.codeError.hidden = true;
  try {
    await createSession(token);
    elements.authDialog.close();
    elements.codeInput.value = '';
    await loadFiles();
  } catch (error) {
    elements.codeError.textContent = error.message || '连接失败';
    elements.codeError.hidden = false;
    elements.codeInput.select();
  } finally {
    elements.connectButton.disabled = false;
  }
});

async function initialize() {
  const currentURL = new URL(window.location.href);
  const token = currentURL.searchParams.get('token');
  if (token) {
    currentURL.searchParams.delete('token');
    history.replaceState({}, '', `${currentURL.pathname}${currentURL.search}${currentURL.hash}`);
    try {
      await createSession(token);
    } catch (error) {
      showAuthDialog(error.message);
    }
  }
  await loadFiles();
}

initialize();
