const $ = selector => document.querySelector(selector);
const registrationForm = $("#registrationForm");
const callHistory = $("#callHistory");
const historyList = $("#historyList");
const historyEmpty = $("#historyEmpty");
const registrationBadge = $("#registrationBadge");
const registrationText = $("#registrationText");
const registrationError = $("#registrationError");
const registerButton = $("#registerButton");
const loginModal = $("#loginModal");
const loginTitle = $("#loginTitle");
const loginDescription = $("#loginDescription");
const profilePicker = $("#profilePicker");
const profileList = $("#profileList");
const profileError = $("#profileError");
const logoutModal = $("#logoutModal");
const logoutButton = $("#logoutButton");
const logoutError = $("#logoutError");
const sipServer = $("#sipServer");
const sipUsername = $("#sipUsername");
const sipPassword = $("#sipPassword");
const destination = $("#destination");
const callStatus = $("#callStatus");
const callTimer = $("#callTimer");
const callButton = $("#call");
const hangupButton = $("#hangup");
const backspace = $("#backspace");
const remoteAudio = $("#remoteAudio");
const mediaStats = $("#mediaStats");
const keypad = $("#keypad");
const codecSelect = $("#codec");
const audioMode = $("#audioMode");
const audioFileControl = $("#audioFileControl");
const audioFile = $("#audioFile");
const chooseAudio = $("#chooseAudio");
const audioFileName = $("#audioFileName");
const traceList = $("#traceList");
const traceDetail = $("#traceDetail");
const traceEmpty = $("#traceEmpty");
const traceModal = $("#traceModal");
const registrationLostModal = $("#registrationLostModal");
const registrationLostMessage = $("#registrationLostMessage");

let peer;
let localStream;
let statusPoll;
let timerInterval;
let connectedAt;
let callStartedAt;
let activeNumber = "";
let isRegistered = false;
let isHangingUp = false;
let isRefreshingStatus = false;
let isCallActive = false;
let currentCallFailed = false;
let callHistoryItems = loadHistory();
let sipProfiles = loadSIPProfiles();
let currentCallID = "";
let audioContext;
let mixDestination;
let microphoneSource;
let selectedAudioBuffer;
let selectedAudioData;
let silenceSource;
let traceEntries = [];
let lastTraceID = 0;
let tracePaused = false;
let traceCallID = "";
let localSipEndpoint = "本机";
let registrationLostAcknowledged = false;

for (const key of ["1", "2", "3", "4", "5", "6", "7", "8", "9", "*", "0", "#"]) {
  const button = document.createElement("button");
  button.type = "button";
  button.textContent = key;
  button.disabled = true;
  button.addEventListener("click", () => {
    destination.value += key;
    destination.dispatchEvent(new Event("input"));
  });
  keypad.append(button);
}

registrationForm.addEventListener("submit", async event => {
  event.preventDefault();
  registrationError.textContent = "";
  setRegistrationState("working", "正在注册…");
  registerButton.disabled = true;
  const payload = {
    server: sipServer.value.trim(),
    username: sipUsername.value.trim(),
    password: sipPassword.value,
  };

  try {
    const response = await fetch("/api/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!response.ok) throw new Error((await response.text()).trim() || "注册失败");
    saveSIPProfile(payload);
    applyRegistration(payload.server, payload.username);
    sipPassword.value = "";
  } catch (error) {
    setRegistrationState("offline", "注册失败");
    registrationError.textContent = error.message;
  } finally {
    registerButton.disabled = false;
  }
});

$("#addProfile").addEventListener("click", showProfileForm);
$("#cancelProfileForm").addEventListener("click", showProfilePicker);

logoutButton.addEventListener("click", () => {
  logoutError.textContent = "";
  showModal(logoutModal);
});

$("#cancelLogout").addEventListener("click", () => hideModal(logoutModal));
$("#confirmLogout").addEventListener("click", logout);
$("#confirmRegistrationLost").addEventListener("click", () => {
  hideModal(registrationLostModal);
  openAccountModal();
});

$("#clearHistory").addEventListener("click", () => {
  callHistoryItems = [];
  localStorage.removeItem("line-one-call-history");
  renderHistory();
});

$("#togglePassword").addEventListener("click", event => {
  const show = sipPassword.type === "password";
  sipPassword.type = show ? "text" : "password";
  event.currentTarget.textContent = show ? "隐藏" : "显示";
});

destination.addEventListener("input", updateDialControls);
backspace.addEventListener("click", () => {
  destination.value = destination.value.slice(0, -1);
  updateDialControls();
});

callButton.addEventListener("click", startCall);
hangupButton.addEventListener("click", () => hangup(true));
$("#simpleMode").addEventListener("click", () => setUIMode("simple"));
$("#proMode").addEventListener("click", () => setUIMode("pro"));
chooseAudio.addEventListener("click", () => audioFile.click());
audioFile.addEventListener("change", prepareAudioFile);
audioMode.addEventListener("change", () => {
  audioFileControl.hidden = audioMode.value !== "file";
  updateDialControls();
});
$("#openTrace").addEventListener("click", () => openTraceModal());
$("#shutdownApp").addEventListener("click", shutdownApp);
$("#closeTrace").addEventListener("click", closeTraceModal);
$("#pauseTrace").addEventListener("click", event => {
  tracePaused = !tracePaused;
  event.currentTarget.textContent = tracePaused ? "继续" : "暂停";
});
$("#exportTrace").addEventListener("click", exportTrace);
$("#clearTrace").addEventListener("click", clearTrace);
$("#traceFilter").addEventListener("change", renderTrace);
setupTraceResizers();

async function startCall() {
  const number = destination.value.trim();
  if (!number || !isRegistered) return;

  try {
    activeNumber = number;
    callStartedAt = Date.now();
    currentCallFailed = false;
    currentCallID = "";
    setCallMode(true);
    setCallStatus(audioMode.value === "file" ? "正在准备播报音频" : "正在准备麦克风", "connecting");
    remoteAudio.muted = audioMode.value === "file";
    const outboundStream = await createOutboundAudio();
    const pc = new RTCPeerConnection();
    peer = pc;
    for (const track of outboundStream.getTracks()) pc.addTrack(track, outboundStream);
    pc.ontrack = event => { remoteAudio.srcObject = event.streams[0]; };
    pc.onconnectionstatechange = async () => {
      if (pc.connectionState === "failed" && peer === pc) {
        currentCallFailed = true;
        setCallStatus("WebRTC 连接失败", "failed");
        await syncCurrentCallIDFromStatus();
        hangup(true, true);
      }
    };

    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    await waitForIce(pc);
    setCallStatus(`正在呼叫 ${number}`, "connecting");

    const response = await fetch("/api/offer", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        destination: number,
        codec: codecSelect.value,
        audioMode: audioMode.value,
        sdp: pc.localDescription.sdp,
      }),
    });
    if (!response.ok) throw new Error((await response.text()).trim());
    const answer = await response.json();
    if (peer !== pc) return;
    await pc.setRemoteDescription({ type: "answer", sdp: answer.sdp });
  } catch (error) {
    currentCallFailed = true;
    setCallStatus(error.message || "呼叫失败", "failed");
    await syncCurrentCallIDFromStatus();
    await hangup(true, true);
  }
}

async function refreshStatus() {
  if (isRefreshingStatus) return;
  isRefreshingStatus = true;
  try {
    const response = await fetch("/api/status", { cache: "no-store" });
    const data = await response.json();
    localSipEndpoint = data.localSip || localSipEndpoint;
    updateRegistrationFromStatus(data);
    currentCallID = data.callId || currentCallID;
    if (data.state === "connected") {
      const announcementLabels = {
        "waiting-media": "等待 VOS 媒体通道",
        playing: "正在向被叫播报音频",
        completed: "播报完成，正在挂断",
      };
      setCallStatus(
        audioMode.value === "file"
          ? announcementLabels[data.announcementState] || "准备播报"
          : "通话中",
        "connected",
      );
      mediaStats.textContent = `RTP 上行 ${data.rtpUp || 0} · 下行 ${data.rtpDown || 0} · ${data.mediaTarget || "等待目标"}`;
      startTimer();
    } else if (data.state === "connecting") {
      setCallStatus("等待对方接听", "connecting");
    } else if (data.state === "failed") {
      currentCallFailed = true;
      setCallStatus(data.error || "呼叫失败", "failed");
      await hangup(true, true);
    } else if (data.state === "idle" && peer && !isHangingUp) {
      await hangup(false);
    }
  } catch {
    setCallStatus("网关连接中断", "failed");
  } finally {
    isRefreshingStatus = false;
  }
}

async function hangup(notifyServer, preserveStatus = false) {
  if (isHangingUp) return;
  isHangingUp = true;

  if (!currentCallID && activeNumber) {
    await syncCurrentCallIDFromTrace(activeNumber);
  }
  recordCall();
  stopTimer();
  if (notifyServer) {
    try {
      await fetch("/api/hangup", { method: "POST", keepalive: true });
    } catch {}
  }
  peer?.close();
  peer = undefined;
  localStream?.getTracks().forEach(track => track.stop());
  localStream = undefined;
  remoteAudio.srcObject = null;
  remoteAudio.muted = false;
  silenceSource?.stop();
  silenceSource = undefined;
  microphoneSource?.disconnect();
  microphoneSource = undefined;
  mixDestination = undefined;
  if (audioContext) {
    audioContext.close();
    audioContext = undefined;
  }
  mediaStats.textContent = "PCMU / G.711 · WebRTC DTLS-SRTP · SIP UDP";
  setCallMode(false);
  destination.value = "";
  if (!preserveStatus) setCallStatus("通话已结束", "idle");

  isHangingUp = false;
}

async function syncCurrentCallIDFromStatus() {
  try {
    const response = await fetch("/api/status", { cache: "no-store" });
    if (!response.ok) return;
    const data = await response.json();
    currentCallID = data.callId || currentCallID;
  } catch {}
}

async function syncCurrentCallIDFromTrace(number) {
  try {
    const response = await fetch("/api/signaling", { cache: "no-store" });
    if (!response.ok) return;
    const entries = await response.json();
    for (let i = entries.length - 1; i >= 0; i--) {
      const entry = entries[i];
      if (entry.callId && entry.summary?.startsWith(`INVITE sip:${number}@`)) {
        currentCallID = entry.callId;
        return;
      }
    }
  } catch {}
}

function applyRegistration(server, username) {
  isRegistered = true;
  registrationLostAcknowledged = false;
  setRegistrationState("online", `${username} 已注册`);
  hideModal(loginModal);
  logoutButton.hidden = false;
  destination.disabled = false;
  setCallStatus("可以拨号", "idle");
  renderHistory();
  updateDialControls();
}

function updateRegistrationFromStatus(data) {
  if (data.registered && !isRegistered) {
    applyRegistration(data.server, data.username);
    return;
  }
  if (!data.registered && isRegistered) {
    isRegistered = false;
    const message = data.error ? `SIP 注册失效，密码可能已变更：${data.error}` : "SIP 注册失效，请重新登录";
    setRegistrationState("offline", "注册已断开");
    registrationError.textContent = message;
    logoutButton.hidden = true;
    destination.disabled = true;
    setCallStatus("SIP 注册失效，请重新登录", "failed");
    updateDialControls();
    showRegistrationLost(message);
  }
}

function showRegistrationLost(message) {
  if (registrationLostAcknowledged) return;
  registrationLostAcknowledged = true;
  registrationLostMessage.textContent = message;
  showModal(registrationLostModal);
}

function setRegistrationState(state, text) {
  registrationBadge.className = `registration-badge ${state}`;
  registrationText.textContent = text;
}

async function logout() {
	const confirmButton = $("#confirmLogout");
	logoutError.textContent = "";
	confirmButton.disabled = true;
  try {
    const response = await fetch("/api/logout", { method: "POST" });
    if (!response.ok) throw new Error((await response.text()).trim() || "退出失败");
    isRegistered = false;
    hideModal(logoutModal);
    logoutButton.hidden = true;
    setRegistrationState("offline", "未注册");
    destination.value = "";
    destination.disabled = true;
    setCallStatus("等待登录", "idle");
    updateDialControls();
    registrationForm.reset();
    openAccountModal();
  } catch (error) {
    logoutError.textContent = error.message;
  } finally {
		confirmButton.disabled = false;
	}
}

async function shutdownApp() {
	if (!confirm("确定退出程序并释放端口吗？")) return;
	try {
		await fetch("/api/shutdown", { method: "POST", keepalive: true });
	} catch {}
	setCallStatus("程序已退出，可以关闭此页面", "idle");
	setRegistrationState("offline", "程序已退出");
	destination.disabled = true;
	setCallMode(false);
	callButton.disabled = true;
	hangupButton.disabled = true;
	logoutButton.hidden = true;
}

function showModal(modal) {
  modal.hidden = false;
  document.body.classList.add("modal-open");
}

function hideModal(modal) {
  modal.hidden = true;
  if (loginModal.hidden && logoutModal.hidden && traceModal.hidden && registrationLostModal.hidden) {
    document.body.classList.remove("modal-open");
  }
}

function loadSIPProfiles() {
  try {
    const value = JSON.parse(localStorage.getItem("line-one-sip-profiles") || "[]");
    return Array.isArray(value)
      ? value.filter(item => item?.server && item?.username && item?.password)
      : [];
  } catch {
    return [];
  }
}

function profileID(profile) {
  return `${profile.username}@${profile.server}`;
}

function saveSIPProfile(profile) {
  const id = profileID(profile);
  const saved = { server: profile.server, username: profile.username, password: profile.password };
  sipProfiles = [saved, ...sipProfiles.filter(item => profileID(item) !== id)];
  localStorage.setItem("line-one-sip-profiles", JSON.stringify(sipProfiles));
}

function openAccountModal() {
  registrationError.textContent = "";
  profileError.textContent = "";
  registrationForm.reset();
  if (sipProfiles.length > 0) {
    showProfilePicker();
  } else {
    showProfileForm();
  }
  showModal(loginModal);
}

function showProfilePicker() {
  loginTitle.textContent = "选择话机配置";
  loginDescription.textContent = "选择一个已保存的 SIP 账号进行注册。";
  registrationForm.hidden = true;
  profilePicker.hidden = false;
  profileError.textContent = "";
  renderSIPProfiles();
}

function showProfileForm(profile) {
  const editing = Boolean(profile);
  loginTitle.textContent = editing ? "编辑 SIP 话机" : "新增 SIP 话机";
  loginDescription.textContent = editing ? "修改注册地址、账号或密码，注册成功后会覆盖原配置。" : "填写 VOS3000 注册信息，注册成功后会保存在本机浏览器。";
  profilePicker.hidden = true;
  registrationForm.hidden = false;
  $("#cancelProfileForm").hidden = sipProfiles.length === 0;
  registrationError.textContent = "";
  if (editing) {
    sipServer.value = profile.server;
    sipUsername.value = profile.username;
    sipPassword.value = profile.password;
  }
  setTimeout(() => sipServer.focus(), 0);
}

function renderSIPProfiles() {
  profileList.replaceChildren();
  for (const profile of sipProfiles) {
    const row = document.createElement("div");
    row.className = "profile-item";

    const identity = document.createElement("div");
    identity.className = "profile-identity";
    const name = document.createElement("strong");
    name.textContent = profile.username;
    const server = document.createElement("span");
    server.textContent = profile.server;
    identity.append(name, server);

    const actions = document.createElement("div");
    actions.className = "profile-actions";
    const useButton = document.createElement("button");
    useButton.type = "button";
    useButton.className = "profile-use";
    useButton.textContent = "注册";
    useButton.addEventListener("click", () => registerSavedProfile(profile, useButton));
    const editButton = document.createElement("button");
    editButton.type = "button";
    editButton.className = "profile-edit";
    editButton.textContent = "编辑";
    editButton.addEventListener("click", () => showProfileForm(profile));
    const removeButton = document.createElement("button");
    removeButton.type = "button";
    removeButton.className = "profile-remove";
    removeButton.textContent = "删除";
    removeButton.addEventListener("click", () => {
      sipProfiles = sipProfiles.filter(item => profileID(item) !== profileID(profile));
      localStorage.setItem("line-one-sip-profiles", JSON.stringify(sipProfiles));
      sipProfiles.length ? renderSIPProfiles() : showProfileForm();
    });
    actions.append(useButton, editButton, removeButton);
    row.append(identity, actions);
    profileList.append(row);
  }
}

async function registerSavedProfile(profile, button) {
  profileError.textContent = "";
  setRegistrationState("working", "正在注册…");
  button.disabled = true;
  try {
    const response = await fetch("/api/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(profile),
    });
    if (!response.ok) throw new Error((await response.text()).trim() || "注册失败");
    applyRegistration(profile.server, profile.username);
  } catch (error) {
    setRegistrationState("offline", "注册失败");
    profileError.textContent = error.message;
  } finally {
    button.disabled = false;
  }
}

function setCallMode(active) {
  isCallActive = active;
  destination.readOnly = active;
  codecSelect.disabled = active;
  audioMode.disabled = active;
  chooseAudio.disabled = active;
  hangupButton.disabled = !active;
  for (const button of keypad.children) button.disabled = active || !isRegistered;
  updateDialControls();
}

function updateDialControls() {
  const inCall = isCallActive;
  const audioReady = audioMode.value !== "file" || Boolean(selectedAudioData);
  callButton.disabled = !isRegistered || inCall || !destination.value.trim() || !audioReady;
  backspace.disabled = !isRegistered || inCall || !destination.value;
  for (const button of keypad.children) button.disabled = !isRegistered || inCall;
}

function setCallStatus(text, state) {
  callStatus.textContent = text;
  callStatus.className = `call-status ${state}`;
}

function startTimer() {
  if (connectedAt) return;
  connectedAt = Date.now();
  callTimer.classList.add("active");
  updateTimer();
  timerInterval = setInterval(updateTimer, 1000);
}

function stopTimer() {
  clearInterval(timerInterval);
  timerInterval = undefined;
  connectedAt = undefined;
  callTimer.textContent = "00:00";
  callTimer.classList.remove("active");
}

function recordCall() {
  if (!callStartedAt || !activeNumber) return;
  const endedAt = Date.now();
  const duration = connectedAt ? Math.max(0, Math.floor((endedAt - connectedAt) / 1000)) : 0;
  callHistoryItems.unshift({
    number: activeNumber,
    timestamp: endedAt,
    duration,
    status: currentCallFailed || !connectedAt ? "failed" : "completed",
    callId: currentCallID,
  });
  callHistoryItems = callHistoryItems.slice(0, 10);
  localStorage.setItem("line-one-call-history", JSON.stringify(callHistoryItems));
  activeNumber = "";
  callStartedAt = undefined;
  currentCallFailed = false;
  currentCallID = "";
  renderHistory();
}

function loadHistory() {
  try {
    const value = JSON.parse(localStorage.getItem("line-one-call-history") || "[]");
    return Array.isArray(value) ? value.slice(0, 10) : [];
  } catch {
    return [];
  }
}

function renderHistory() {
  historyList.replaceChildren();
  historyEmpty.hidden = callHistoryItems.length > 0;
  for (const item of callHistoryItems) {
    const row = document.createElement("div");
    row.className = "history-item";
    row.tabIndex = 0;
    row.setAttribute("role", "button");
    row.setAttribute("aria-label", `填入号码 ${item.number}`);
    row.setAttribute("aria-disabled", String(!isRegistered || isCallActive));
    row.addEventListener("click", () => {
      if (!isRegistered || isCallActive) return;
      destination.value = item.number;
      updateDialControls();
      destination.focus();
    });
    row.addEventListener("keydown", event => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        if (!isRegistered || isCallActive) return;
        destination.value = item.number;
        updateDialControls();
        destination.focus();
      }
    });

    const icon = document.createElement("span");
    icon.className = `history-icon ${item.status === "failed" ? "failed" : ""}`;
    icon.textContent = item.status === "failed" ? "×" : "↗";

    const copy = document.createElement("div");
    copy.className = "history-copy";
    const number = document.createElement("strong");
    number.textContent = item.number;
    const meta = document.createElement("span");
    meta.textContent = `${item.status === "failed" ? "未接通" : "已呼出"} · ${formatDate(item.timestamp)}`;
    copy.append(number, meta);

    const actions = document.createElement("span");
    actions.className = "history-actions";
    const duration = document.createElement("span");
    duration.className = "history-duration";
    duration.textContent = item.status === "failed" ? "—" : formatDuration(item.duration);
    actions.append(duration);
    if (item.callId) {
      const traceButton = document.createElement("button");
      traceButton.type = "button";
      traceButton.className = "history-trace";
      traceButton.textContent = "信令";
      traceButton.addEventListener("click", event => {
        event.stopPropagation();
        openHistoryTrace(item);
      });
      traceButton.addEventListener("keydown", event => event.stopPropagation());
      actions.append(traceButton);
    }
    row.append(icon, copy, actions);
    const wrapper = document.createElement("div");
    wrapper.className = "history-row";
    wrapper.append(row);
    historyList.append(wrapper);
  }
}

function formatDate(timestamp) {
  const date = new Date(timestamp);
  const today = new Date();
  const sameDay = date.toDateString() === today.toDateString();
  return sameDay
    ? date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })
    : date.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" });
}

function formatDuration(seconds) {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  return hours ? `${pad(hours)}:${pad(minutes)}:${pad(rest)}` : `${pad(minutes)}:${pad(rest)}`;
}

function updateTimer() {
  if (!connectedAt) return;
  const seconds = Math.floor((Date.now() - connectedAt) / 1000);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  callTimer.textContent = hours
    ? `${pad(hours)}:${pad(minutes)}:${pad(rest)}`
    : `${pad(minutes)}:${pad(rest)}`;
}

function pad(value) {
  return String(value).padStart(2, "0");
}

function waitForIce(pc) {
  if (pc.iceGatheringState === "complete") return Promise.resolve();
  return new Promise(resolve => {
    const listener = () => {
      if (pc.iceGatheringState === "complete") {
        pc.removeEventListener("icegatheringstatechange", listener);
        resolve();
      }
    };
    pc.addEventListener("icegatheringstatechange", listener);
  });
}

async function createOutboundAudio() {
  if (audioMode.value === "microphone") {
    localStream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      video: false,
    });
    return localStream;
  }

  // File announcements are sent as RTP by the gateway. A silent browser track
  // keeps the WebRTC audio m-line active so return audio can still negotiate.
  audioContext = new AudioContext();
  await audioContext.resume();
  mixDestination = audioContext.createMediaStreamDestination();
  silenceSource = audioContext.createOscillator();
  const silenceGain = audioContext.createGain();
  silenceGain.gain.value = 0;
  silenceSource.connect(silenceGain).connect(mixDestination);
  silenceSource.start();
  return mixDestination.stream;
}

async function prepareAudioFile() {
  const file = audioFile.files[0];
  if (!file) return;
  try {
    setAudioFileLabel("正在处理音频…");
    if (/\.wav$/i.test(file.name) || file.type === "audio/wav" || file.type === "audio/x-wav") {
      const response = await fetch("/api/audio?format=wav", {
        method: "POST",
        headers: { "Content-Type": "audio/wav" },
        body: await file.arrayBuffer(),
      });
      if (response.ok) {
        selectedAudioData = { uploaded: true };
        setAudioFileLabel(file.name);
        updateDialControls();
        return;
      }
    }
    const decodeContext = new AudioContext();
    const decoded = await decodeContext.decodeAudioData(await file.arrayBuffer());
    const mono = mixToMono(decoded);
    const floats = resampleLinear(mono, decoded.sampleRate, 8000);
    await decodeContext.close();
    const pcm = new Int16Array(floats.length);
    for (let i = 0; i < floats.length; i++) {
      const sample = Math.max(-1, Math.min(1, floats[i]));
      pcm[i] = sample < 0 ? sample * 32768 : sample * 32767;
    }
    const response = await fetch("/api/audio", {
      method: "POST",
      headers: { "Content-Type": "application/octet-stream" },
      body: pcm.buffer,
    });
    if (!response.ok) throw new Error((await response.text()).trim());
    selectedAudioData = pcm.buffer;
    selectedAudioBuffer = undefined;
    setAudioFileLabel(file.name);
    updateDialControls();
  } catch (error) {
    selectedAudioData = undefined;
    selectedAudioBuffer = undefined;
    setAudioFileLabel(`音频处理失败：${error.message || "格式不支持"}`);
    updateDialControls();
  }
}

function setAudioFileLabel(text) {
  chooseAudio.textContent = text;
  audioFileName.textContent = text;
  chooseAudio.title = text;
}

function mixToMono(buffer) {
  const mono = new Float32Array(buffer.length);
  for (let channel = 0; channel < buffer.numberOfChannels; channel++) {
    const data = buffer.getChannelData(channel);
    for (let i = 0; i < data.length; i++) mono[i] += data[i] / buffer.numberOfChannels;
  }
  return mono;
}

function resampleLinear(input, sourceRate, targetRate) {
  if (sourceRate === targetRate) return input;
  const output = new Float32Array(Math.max(1, Math.round(input.length * targetRate / sourceRate)));
  const ratio = sourceRate / targetRate;
  for (let i = 0; i < output.length; i++) {
    const position = i * ratio;
    const left = Math.floor(position);
    const right = Math.min(left + 1, input.length - 1);
    const fraction = position - left;
    output[i] = input[left] * (1 - fraction) + input[right] * fraction;
  }
  return output;
}

function setUIMode(mode) {
  const simple = mode === "simple";
  document.body.classList.toggle("simple-mode", simple);
  $("#simpleMode").classList.toggle("active", simple);
  $("#proMode").classList.toggle("active", !simple);
  localStorage.setItem("line-one-ui-mode", mode);
}

async function refreshTrace() {
  if (tracePaused || traceCallID) return;
  try {
    const response = await fetch(`/api/signaling?after=${lastTraceID}`, { cache: "no-store" });
    if (!response.ok) return;
    const incoming = await response.json();
    if (!incoming.length) return;
    traceEntries.push(...incoming);
    traceEntries = traceEntries.slice(-500);
    lastTraceID = traceEntries[traceEntries.length - 1].id;
    renderTrace();
  } catch {}
}

function renderTrace() {
  const filter = $("#traceFilter").value;
  const visible = traceEntries.filter(entry => filter === "all" || entry.direction === filter);
  traceList.replaceChildren();
  $("#traceCount").textContent = traceCallID ? `历史 · ${visible.length} 条` : `${visible.length} 条`;
  traceEmpty.hidden = visible.length > 0;
  if (!visible.length) {
    return;
  }
  visible.forEach((entry, index) => {
    const row = document.createElement("div");
    row.className = "trace-row";
    const sequence = document.createElement("span");
    sequence.className = "trace-sequence";
    sequence.textContent = String(index + 1);
    const callerLane = document.createElement("div");
    const calleeLane = document.createElement("div");
    callerLane.className = `trace-lane ${entry.direction === "out" ? "" : "empty"}`;
    calleeLane.className = `trace-lane ${entry.direction === "in" ? "" : "empty"}`;
    const flow = entry.direction === "out" ? callerLane : calleeLane;
    const endpoints = document.createElement("div");
    endpoints.className = "trace-endpoints";
    const local = localSipEndpoint;
    const remote = entry.peer || "SIP 服务器";
    const left = document.createElement("span");
    const right = document.createElement("span");
    left.textContent = entry.direction === "out" ? local : remote;
    right.textContent = entry.direction === "out" ? remote : local;
    endpoints.append(left, right);
    const arrow = document.createElement("div");
    arrow.className = `trace-arrow ${entry.direction}`;
    const summary = document.createElement("strong");
    summary.textContent = shortSignalName(entry.summary);
    arrow.append(summary);
    flow.append(endpoints, arrow);
    const time = document.createElement("div");
    time.className = "trace-real-time";
    time.textContent = formatTraceTime(entry.time);
    row.append(sequence, callerLane, calleeLane, time);
    row.addEventListener("click", () => {
      for (const child of traceList.children) child.classList.remove("active");
      row.classList.add("active");
      traceDetail.textContent = `${entry.direction === "out" ? "SEND" : "RECV"} ${entry.peer}\nCall-ID: ${entry.callId || "—"}\n\n${entry.message}`;
    });
    traceList.append(row);
  });
  traceList.parentElement.scrollTop = traceList.parentElement.scrollHeight;
}

function formatTraceTime(value) {
  const date = new Date(value);
  const datePart = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  const millis = String(date.getMilliseconds()).padStart(3, "0");
  const timePart = `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${millis}`;
  return `${datePart}\n${timePart}`;
}

function setupTraceResizers() {
  const workspace = $(".trace-workspace");
  const flowPane = $(".trace-flow-pane");
  const paneHandle = $("#tracePaneResize");
  const laneHandle = $("#laneResize");

  paneHandle.addEventListener("pointerdown", event => {
    event.preventDefault();
    const move = moveEvent => {
      const rect = workspace.getBoundingClientRect();
      const width = Math.max(500, Math.min(rect.width - 320, moveEvent.clientX - rect.left));
      workspace.style.setProperty("--flow-width", `${width}px`);
      positionLaneHandle();
    };
    const stop = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
  });

  laneHandle.addEventListener("pointerdown", event => {
    event.preventDefault();
    const move = moveEvent => {
      const rect = flowPane.getBoundingClientRect();
      const laneLeft = 54;
      const laneWidth = rect.width - 259;
      const position = Math.max(laneWidth * .25, Math.min(laneWidth * .75, moveEvent.clientX - rect.left - laneLeft));
      flowPane.style.setProperty("--caller-width", `${position}px`);
      laneHandle.style.left = `${laneLeft + position}px`;
    };
    const stop = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
  });

  function positionLaneHandle() {
    const rect = flowPane.getBoundingClientRect();
    if (!rect.width) return;
    const laneWidth = rect.width - 259;
    const callerWidth = Math.max(laneWidth * .25, laneWidth / 2);
    flowPane.style.setProperty("--caller-width", `${callerWidth}px`);
    laneHandle.style.left = `${54 + callerWidth}px`;
  }
  window.positionTraceHandles = positionLaneHandle;
  window.addEventListener("resize", positionLaneHandle);
}

function shortSignalName(summary) {
  const line = String(summary || "").trim();
  if (!line) return "SIP";
  if (line.startsWith("SIP/2.0 ")) {
    return line.slice("SIP/2.0 ".length);
  }
  return line.split(/\s+/, 1)[0].toUpperCase();
}

async function openHistoryTrace(item) {
  setUIMode("pro");
  traceCallID = item.callId;
  const response = await fetch(`/api/signaling?callId=${encodeURIComponent(item.callId)}`, { cache: "no-store" });
  traceEntries = response.ok ? await response.json() : [];
  renderTrace();
  openTraceModal(`${item.number} · 历史信令`);
}

async function clearTrace() {
  if (traceCallID) {
    traceCallID = "";
    traceEntries = [];
    lastTraceID = 0;
  } else {
    await fetch("/api/signaling", { method: "DELETE" }).catch(() => {});
    traceEntries = [];
    lastTraceID = 0;
  }
  traceDetail.textContent = "选择一条信令查看原始报文";
  renderTrace();
}

function exportTrace() {
  const filter = $("#traceFilter").value;
  const visible = traceEntries.filter(entry => filter === "all" || entry.direction === filter);
  if (!visible.length) return;
  const content = visible.map(entry => {
    const direction = entry.direction === "out" ? "SEND" : "RECV";
    return [
      `[${formatTraceTime(entry.time).replace("\n", " ")}] ${direction} ${entry.peer || ""}`,
      `Call-ID: ${entry.callId || ""}`,
      entry.message || entry.summary || "",
    ].join("\n");
  }).join("\n\n---\n\n");
  const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `sip-trace-${new Date().toISOString().replace(/[:.]/g, "-")}.txt`;
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function openTraceModal(title = "信令交互") {
  $("#traceTitle").textContent = title;
  showModal(traceModal);
  requestAnimationFrame(() => window.positionTraceHandles?.());
}

function closeTraceModal() {
  hideModal(traceModal);
  if (traceCallID) {
    traceCallID = "";
    traceEntries = [];
    lastTraceID = 0;
    traceDetail.textContent = "选择一条信令查看原始报文";
    renderTrace();
  }
}

openAccountModal();
refreshStatus();
statusPoll = setInterval(refreshStatus, 1000);
setInterval(refreshTrace, 700);
setUIMode(localStorage.getItem("line-one-ui-mode") || "pro");
