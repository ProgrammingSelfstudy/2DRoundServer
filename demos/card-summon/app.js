(() => {
  'use strict';

  // The renderer consumes a result list. Replace createDemoResult with an adapter
  // to confirmed server results; never draw again in an animation callback.
  const CARDS = Array.from({ length: 10 }, (_, index) => ({
    id: String(index + 1).padStart(2, '0'), name: '罗罗诺亚·索隆',
    subtitle: 'THREE SWORD STYLE', rarity: 'SSR',
  }));
  const $ = id => document.getElementById(id);
  const el = Object.fromEntries([
    'stage', 'stageStatus', 'drawButton', 'drawLabel', 'controlNote', 'replayButton',
    'packCount', 'skipButton', 'cardPresentation', 'revealCard', 'revealCounter',
    'revealedImage', 'rarityBadge', 'cardName', 'cardSubtitle', 'cardSerial',
    'revealHint', 'lightBurst', 'resultsPanel', 'resultsGrid', 'resultsCount',
    'resultsNote', 'againButton', 'announcement', 'soundToggle', 'soundLabel',
    'packFleet', 'fleetRig', 'fleetCaption', 'openingProgress', 'openingProgressLabel',
    'openingProgressDots', 'rotationToggle', 'frontViewButton',
    'singleBackButton', 'resultsBackButton',
  ].map(id => [id, $(id)]));
  const quantityButtons = [...document.querySelectorAll('.quantity-option')];
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
  let selectedCount = 10;
  let phase = 'loading';
  let results = [];
  let previousResults = [];
  let revealedIndex = 0;
  let runId = 0;
  let changingCard = false;
  let loaded = false;
  let soundEnabled = false;
  let audioContext;
  let fleetPacks = [];
  const fleetImages = [];
  let openingDots = [];
  let orbitAngle = 0;
  let orbitRunning = true;
  let orbitRadius = 0;
  let orbitScale = 1;
  let orbitDrag = null;
  let orbitAnimation = null, lightAnimation;
  const ORBIT_DURATION = 30000;
  const isOrbitPhase = () => phase === 'idle' || phase === 'assembling';
  const sleeping = ms => new Promise(resolve => setTimeout(resolve, reducedMotion.matches ? 0 : ms));
  const isBusy = () => !['idle', 'results', 'error'].includes(phase) && !(phase === 'revealing' && results.length === 1);
  // Keep loaded bytes available for images created later (results, replay,
  // reflections), even when the local preview server has been stopped.
  const assetSources = new Map(Object.entries(window.__SUMMON_ASSETS__ || {}));
  const asset = path => assetSources.get(path) || path;
  const cardImage = () => asset('assets/zoro-card-art.png');

  // Opt-in local QA: publish frame intervals to the DOM without console spam.
  // Disabled in normal use; measurements exclude time spent in a hidden tab.
  if (new URLSearchParams(location.search).has('profile')) {
    const output = document.createElement('output');
    output.id = 'frameStats'; output.hidden = true; document.body.append(output);
    const samples = new Map();
    const orbitWrites = { idle: 0, assembling: 0 };
    new MutationObserver(records => {
      if (!(phase in orbitWrites)) return;
      orbitWrites[phase] += records.filter(record => record.target === el.fleetRig || record.target.classList.contains('fleet-pose')).length;
    }).observe(el.fleetRig, { subtree: true, attributes: true, attributeFilter: ['style'] });
    let previousTime = 0, previousPhase = phase;
    document.addEventListener('visibilitychange', () => { previousTime = 0; previousPhase = phase; });
    const publish = () => {
      const report = {};
      samples.forEach((times, name) => {
        const sorted = [...times].sort((a, b) => a - b);
        report[name] = { frames: times.length, p95: +(sorted[Math.floor((sorted.length - 1) * .95)] || 0).toFixed(1), over34ms: times.filter(ms => ms > 34).length, max: +Math.max(...times).toFixed(1) };
        if (name in orbitWrites) report[name].styleWrites = orbitWrites[name];
      });
      output.textContent = JSON.stringify(report);
    };
    const sample = now => {
      if (document.hidden) previousTime = 0;
      else {
        if (previousTime) {
          const times = samples.get(previousPhase) || [];
          if (times.length < 3600) times.push(now - previousTime);
          samples.set(previousPhase, times);
        }
        if (previousPhase !== phase) publish();
        previousTime = now; previousPhase = phase;
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  }

  function announce(message) { el.announcement.textContent = message; }

  function setPhase(next) {
    if (!['idle', 'assembling', 'loading'].includes(next)) freezeOrbit(next === 'arming');
    el.stage.classList.remove(`is-${phase}`);
    phase = next;
    el.stage.dataset.phase = next;
    el.stage.classList.add(`is-${next}`);
    if (isOrbitPhase() || next === 'loading') ensureOrbit();
    else fleetPacks.forEach(pose => pose.style.removeProperty('transform'));
    syncMotion();
    el.stage.classList.toggle('busy', !['idle', 'loading', 'error'].includes(next));
    const busy = isBusy();
    el.drawButton.disabled = busy || (!loaded && next !== 'error');
    quantityButtons.forEach(button => { button.disabled = busy; });
    el.replayButton.disabled = busy || !previousResults.length;
    el.rotationToggle.disabled = next !== 'idle' || el.stage.dataset.mode !== 'ten';
    el.frontViewButton.disabled = next !== 'idle' || el.stage.dataset.mode !== 'ten';
    el.skipButton.hidden = !busy || !['focusing', 'cutting', 'opening', 'assembling', 'arming', 'multi-opening', 'gathering', 'revealing'].includes(next);
    el.singleBackButton.hidden = !(next === 'revealing' && results.length === 1);
    el.drawLabel.textContent = next === 'error' ? '重新加载卡面' : busy ? '正在开包…' : next === 'revealing' ? '再次开启' : '开启卡包';
  }

  function updateCount(count) {
    selectedCount = count;
    quantityButtons.forEach(button => {
      const selected = Number(button.dataset.count) === count;
      button.classList.toggle('selected', selected);
      button.setAttribute('aria-pressed', String(selected));
    });
    if (phase === 'idle' || phase === 'error') setMode(count);
    el.controlNote.textContent = `${count} 个索隆卡包 · ${count === 10 ? '环形旋转' : '单包开启'} · 模拟开启`;
  }

  function buildFleet() {
    const template = $('packModel');
    fleetPacks = Array.from({ length: 10 }, (_, index) => {
      const pose = document.createElement('div');
      pose.className = 'fleet-pose'; pose.dataset.state = 'sealed';
      pose.dataset.pack = String(index + 1); pose.style.setProperty('--index', index);
      const shadow = document.createElement('div'); shadow.className = 'fleet-base-shadow';
      const card = document.createElement('div'); card.className = 'fleet-card';
      for (const faceName of ['back', 'front']) {
        const face = document.createElement('div'); face.className = `fleet-card-face card-${faceName}-face`;
        const image = document.createElement('img'); image.alt = '';
        image.src = faceName === 'back' ? asset('assets/zoro-card-back.svg') : cardImage(CARDS[index]);
        if (faceName === 'front') fleetImages.push(image);
        face.append(image); card.append(face);
      }
      const sleeve = document.createElement('div'); sleeve.className = 'fleet-sleeve';
      const model = template.cloneNode(true); model.removeAttribute('id'); model.className = 'fleet-model';
      const number = document.createElement('span'); number.className = 'fleet-number'; number.textContent = String(index + 1).padStart(2, '0');
      const reflection = document.createElement('div'); reflection.className = 'pack-reflection';
      for (const side of ['front', 'back']) {
        const face = document.createElement('div'); face.className = `reflection-face reflection-${side}`;
        const mirror = document.createElement('img'); mirror.alt = ''; mirror.src = asset(`assets/zoro-pack-${side}-hd.svg`);
        face.append(mirror); reflection.append(face);
      }
      model.append(number); sleeve.append(model); pose.append(shadow, card, sleeve, reflection);
      el.fleetRig.append(pose); return pose;
    });
    openingDots = fleetPacks.map(() => {
      const dot = document.createElement('span'); dot.className = 'opening-dot';
      el.openingProgressDots.append(dot); return dot;
    });
    layoutFleet();
  }

  function layoutFleet() {
    // Resize is infrequent. Keep the current angle while rebuilding the radius.
    freezeOrbit();
    const width = el.stage.clientWidth;
    const height = el.stage.clientHeight;
    const mobile = window.matchMedia('(max-width: 780px)').matches;
    const packWidth = mobile ? Math.min(width * .535, height * .38) : width * .535;
    orbitRadius = packWidth * 1.78;
    orbitScale = packWidth / 230;
    el.stage.style.setProperty('--stack-scale', orbitScale);
    el.stage.style.setProperty('--single-scale', orbitScale);
    el.stage.style.setProperty('--scene-perspective', `${width * 2.35}px`);
    el.stage.style.setProperty('--gather-offset', `${el.stage.clientHeight * -.02}px`);
    el.stage.style.setProperty('--mobile-card-width', `${Math.max(90, Math.min(220, width * .62, (height - 132) * 480 / 680))}px`);
    if (isOrbitPhase() || phase === 'loading') ensureOrbit();
    syncMotion();
  }

  function normalizeAngle(angle) { return ((angle % 360) + 360) % 360; }

  function currentOrbitAngle() {
    const time = orbitAnimation?.currentTime;
    return typeof time === 'number' ? normalizeAngle(time / ORBIT_DURATION * 360) : orbitAngle;
  }

  function placeRing(offset = 0) {
    fleetPacks.forEach((pose, index) => {
      const angle = offset + index * 36;
      const radians = angle * Math.PI / 180;
      const depth = Math.cos(radians);
      // Keep the equivalent angle nearest the -6° opening pose, so gathering
      // never interpolates through an unnecessary full rotation.
      const yaw = ((angle + 186) % 360 + 360) % 360 - 186;
      pose.style.transform = `translate3d(${Math.sin(radians) * orbitRadius}px,0px,${(depth - 1) * orbitRadius}px) rotateY(${yaw}deg) rotateZ(0deg) scale(${orbitScale})`;
    });
  }

  function ensureOrbit() {
    if (orbitAnimation || !fleetPacks.length) return;
    placeRing();
    el.fleetRig.style.transform = 'none';
    // Rotate the entire fixed ring around its centre behind the front package.
    // These native transform keyframes run without per-frame JavaScript writes.
    orbitAnimation = el.fleetRig.animate([
      { transform: `translateZ(${-orbitRadius}px) rotateY(0deg) translateZ(${orbitRadius}px)` },
      { transform: `translateZ(${-orbitRadius}px) rotateY(360deg) translateZ(${orbitRadius}px)` },
    ], { duration: ORBIT_DURATION, iterations: Infinity, easing: 'linear', fill: 'both' });
    orbitAnimation.pause();
    orbitAnimation.currentTime = normalizeAngle(orbitAngle) / 360 * ORBIT_DURATION;
    el.stage.dataset.orbitRenderer = 'compositor';
  }

  function seekOrbit(angle) {
    orbitAngle = normalizeAngle(angle);
    ensureOrbit();
    orbitAnimation.currentTime = orbitAngle / 360 * ORBIT_DURATION;
    el.stage.dataset.orbitAngle = orbitAngle.toFixed(2);
  }

  function freezeOrbit(bakeWorldPose = false) {
    if (!orbitAnimation) return;
    orbitAngle = currentOrbitAngle();
    orbitAnimation.cancel(); orbitAnimation = null;
    el.fleetRig.style.transform = 'none';
    if (bakeWorldPose) {
      // Preserve the exact world positions before handing over to the existing
      // opening transition. One style commit at this boundary, never per frame.
      placeRing(orbitAngle);
      void getComputedStyle(fleetPacks[0]).transform;
    }
    el.stage.dataset.orbitAngle = orbitAngle.toFixed(2);
    el.stage.dataset.orbitState = 'stopped';
  }

  function setMode(count) {
    const ten = count === 10;
    el.stage.dataset.mode = ten ? 'ten' : 'single';
    el.packFleet.hidden = !ten;
    el.fleetCaption.hidden = !ten || phase !== 'idle';
    el.packCount.hidden = true;
    el.openingProgress.hidden = true;
    el.stage.style.setProperty('--fleet-y', '-5deg');
    el.stage.style.setProperty('--fleet-x', '3deg');
    fleetPacks.forEach(pose => { pose.dataset.state = 'sealed'; pose.classList.remove('dealt'); });
    openingDots.forEach(dot => { dot.className = 'opening-dot'; });
    el.rotationToggle.disabled = !ten || phase !== 'idle';
    el.frontViewButton.disabled = !ten || phase !== 'idle';
    if (ten && isOrbitPhase()) ensureOrbit();
    syncMotion();
    if (phase === 'idle') el.stageStatus.textContent = ten ? '十包环绕，三刀流登场' : '封存的奇遇，等待开启';
  }

  async function openFleetPack(index, token) {
    await sleeping(index * 155);
    if (token !== runId) return;
    const pose = fleetPacks[index];
    pose.dataset.state = 'cutting';
    openingDots[index].classList.add('active');
    el.openingProgressLabel.textContent = `正在启封 ${String(index + 1).padStart(2, '0')} / 10`;
    if (index % 2 === 0) sound('cut');
    await sleeping(600);
    if (token !== runId) return;
    pose.dataset.state = 'opening';
    await sleeping(1050);
    if (token !== runId) return;
    pose.dataset.state = 'opened';
    openingDots[index].classList.remove('active'); openingDots[index].classList.add('done');
  }

  async function animateFleet(token) {
    el.fleetCaption.hidden = true;
    el.openingProgress.hidden = false;
    el.openingProgressLabel.textContent = '10 个索隆卡包 · 环绕登场';
    fleetPacks.forEach((pose, index) => {
      pose.dataset.cardId = results[index].id;
      const source = cardImage(results[index]);
      if (fleetImages[index].getAttribute('src') !== source) fleetImages[index].src = source;
    });
    await sleeping(1500);
    if (token !== runId) return;
    setPhase('arming');
    el.stageStatus.textContent = '封口已就位，准备逐一开启';
    await sleeping(850);
    if (token !== runId) return;
    setPhase('multi-opening');
    el.stageStatus.textContent = '十个卡包，依次回应光的轨迹';
    announce('正在依次撕开十个独立卡包');
    await Promise.all(fleetPacks.map((_, index) => openFleetPack(index, token)));
    if (token !== runId) return;
    el.openingProgressLabel.textContent = '十包已启封';
    await sleeping(450);
    if (token !== runId) return;
    setPhase('gathering');
    el.stageStatus.textContent = '所有故事，汇聚于此';
    el.openingProgressLabel.textContent = '收拢卡片，准备揭晓';
    sound('start');
    await sleeping(1450);
    if (token !== runId) return;
    el.cardPresentation.hidden = false;
    el.cardPresentation.classList.add('entering');
    showCard(0);
    // Keep gathering until the first flip settles, so it cannot be clicked early.
    await sleeping(1050);
    if (token !== runId) return;
    setPhase('revealing');
    el.cardPresentation.classList.remove('entering');
    el.revealCard.focus({ preventScroll: true });
  }

  function createDemoResult(count) {
    if (count === 1) return [{ ...CARDS[Math.floor(Math.random() * CARDS.length)] }];
    // Ten collectible serials share the Zoro illustration; this is a visual demo.
    const deck = CARDS.map(card => ({ ...card }));
    for (let i = deck.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [deck[i], deck[j]] = [deck[j], deck[i]];
    }
    return deck;
  }

  async function ensureAudio() {
    if (!soundEnabled) return;
    try {
      const Audio = window.AudioContext || window.webkitAudioContext;
      if (!Audio) throw new Error('Audio unavailable');
      if (!audioContext) audioContext = new Audio();
      if (audioContext.state === 'suspended') await audioContext.resume();
    } catch {
      soundEnabled = false;
      el.soundToggle.setAttribute('aria-pressed', 'false');
      el.soundLabel.textContent = '声音不可用';
      el.soundToggle.setAttribute('aria-label', '重试开启音效');
    }
  }

  function sound(type, rare = false) {
    if (!soundEnabled || audioContext?.state !== 'running') return;
    const notes = type === 'reveal' ? (rare ? [523.25, 659.25, 783.99, 1046.5] : [440, 554.37, 659.25])
      : type === 'cut' ? [1800, 1100, 650] : type === 'start' ? [261.63, 392] : [880];
    notes.forEach((frequency, index) => {
      const at = audioContext.currentTime + index * (type === 'cut' ? .04 : .085);
      const oscillator = audioContext.createOscillator();
      const gain = audioContext.createGain();
      oscillator.type = type === 'cut' ? 'triangle' : 'sine';
      oscillator.frequency.setValueAtTime(frequency, at);
      if (type === 'cut') oscillator.frequency.exponentialRampToValueAtTime(150, at + .17);
      gain.gain.setValueAtTime(0, at);
      gain.gain.linearRampToValueAtTime(type === 'cut' ? .018 : .035, at + .012);
      gain.gain.exponentialRampToValueAtTime(.001, at + .48);
      oscillator.connect(gain); gain.connect(audioContext.destination);
      oscillator.start(at); oscillator.stop(at + .5);
      oscillator.onended = () => { oscillator.disconnect(); gain.disconnect(); };
    });
  }

  function flash() {
    lightAnimation?.cancel();
    if (reducedMotion.matches) return;
    lightAnimation = el.lightBurst.animate([
      { opacity: 0, transform: 'translate(-50%,-50%) scale(.5) rotate(-15deg)' },
      { opacity: 1, offset: .35 },
      { opacity: 0, transform: 'translate(-50%,-50%) scale(1.5) rotate(20deg)' },
    ], { duration: 1400, easing: 'ease-out' });
  }

  function showCard(index) {
    const card = results[index];
    el.cardPresentation.dataset.rarity = card.rarity;
    if (el.revealedImage.getAttribute('src') !== cardImage(card)) el.revealedImage.src = cardImage(card);
    el.revealedImage.alt = `${card.name}，${card.rarity} 品质索隆主题卡`;
    el.rarityBadge.textContent = card.rarity;
    el.cardName.textContent = card.name;
    el.cardSubtitle.textContent = card.subtitle;
    el.cardSerial.textContent = `ZORO COLLECTION · ${card.id.padStart(3, '0')}`;
    el.revealCounter.replaceChildren(document.createTextNode(String(index + 1).padStart(2, '0') + ' '));
    const total = document.createElement('span'); total.textContent = `/ ${String(results.length).padStart(2, '0')}`;
    el.revealCounter.append(total);
    const isLast = index === results.length - 1;
    const single = results.length === 1;
    const nextAction = isLast ? '查看本次收获' : '查看下一张';
    el.revealHint.hidden = single;
    el.revealCounter.hidden = single;
    el.revealCard.disabled = single;
    el.revealHint.textContent = `轻触卡片，${nextAction} →`;
    el.revealCard.setAttribute('aria-label', single ? `${card.name}，${card.rarity}，卡片已揭晓` : `${card.name}，${card.rarity}，第 ${index + 1} 张，共 ${results.length} 张。${nextAction}`);
    el.revealCard.style.setProperty('--card-y', '0deg');
    el.revealCard.style.setProperty('--card-x', '0deg');
    el.revealCard.style.setProperty('--card-shine', '-25%');
    el.stageStatus.textContent = card.rarity === 'SSR' ? '三刀流 · 索隆' : '索隆卡片已揭晓';
    if (results.length === 10) {
      fleetPacks.forEach((pose, i) => pose.classList.toggle('dealt', i <= index));
      openingDots.forEach((dot, i) => {
        dot.classList.toggle('active', i === index); dot.classList.toggle('done', i < index);
      });
    }
    announce(`第 ${index + 1} 张，共 ${results.length} 张：${card.rarity} ${card.name}`);
    // Put the impact on the face turn, not on the still-hidden card back.
    const effectToken = runId;
    void sleeping(420).then(() => {
      if (effectToken !== runId || index !== revealedIndex) return;
      sound('reveal', card.rarity === 'SSR');
      if (card.rarity === 'SSR') flash();
    });
  }

  async function start(resultList) {
    if (!loaded || isBusy()) return;
    const token = ++runId;
    // Freeze the completed draw before starting any visual work.
    results = resultList.map(card => ({ ...card }));
    previousResults = results.map(card => ({ ...card }));
    revealedIndex = 0;
    changingCard = false;
    el.resultsPanel.hidden = true;
    el.resultsGrid.replaceChildren();
    el.cardPresentation.hidden = true;
    el.cardPresentation.classList.remove('entering', 'turning', 'leaving');
    el.packCount.hidden = true;
    lightAnimation?.cancel();
    el.stage.style.setProperty('--rotate-y', '-15deg');
    el.stage.style.setProperty('--rotate-x', '3deg');
    setMode(results.length);
    setPhase(results.length === 10 ? 'assembling' : 'focusing');
    el.stageStatus.textContent = results.length === 10 ? '十个索隆卡包，即将开启' : '索隆，准备登场';
    announce(`开始${results.length === 10 ? '十包连开' : '单包开启'}`);
    el.skipButton.focus({ preventScroll: true });
    // Compact portrait layouts already show the stage and controls together.
    // Only bring an offscreen stage into view in short, scrollable layouts.
    if (window.matchMedia('(max-width: 780px)').matches) {
      const box = el.stage.getBoundingClientRect();
      if (box.bottom <= 0 || box.top >= window.innerHeight) el.stage.scrollIntoView({ behavior: 'instant', block: 'center' });
    }
    void ensureAudio().then(() => { if (token === runId) sound('start'); });
    if (results.length === 10) { await animateFleet(token); return; }
    await sleeping(1170);
    if (token !== runId) return;
    setPhase('cutting');
    el.stageStatus.textContent = '沿着光，打开封存的故事';
    sound('cut');
    await sleeping(850);
    if (token !== runId) return;
    setPhase('opening');
    await sleeping(450);
    if (token !== runId) return;
    el.cardPresentation.hidden = false;
    el.cardPresentation.classList.add('entering');
    showCard(0);
    await sleeping(1050);
    if (token !== runId) return;
    setPhase('revealing');
    el.cardPresentation.classList.remove('entering');
    el.drawButton.focus({ preventScroll: true });
  }

  async function advance() {
    if (phase !== 'revealing' || changingCard || results.length === 1) return;
    if (revealedIndex === results.length - 1) { showResults(); return; }
    changingCard = true;
    const token = runId;
    el.cardPresentation.classList.add('leaving');
    await sleeping(280);
    if (token !== runId) return;
    el.cardPresentation.classList.remove('leaving');
    revealedIndex++;
    el.cardPresentation.classList.add('turning');
    showCard(revealedIndex);
    await sleeping(720);
    if (token !== runId) return;
    el.cardPresentation.classList.remove('turning');
    changingCard = false;
  }

  function showResults() {
    if (!results.length) return;
    ++runId; // Invalidates every pending animation continuation, including skip.
    changingCard = false;
    lightAnimation?.cancel();
    el.packCount.hidden = true;
    el.openingProgress.hidden = true;
    el.fleetCaption.hidden = true;
    if (results.length === 1) {
      el.resultsPanel.hidden = true;
      el.cardPresentation.hidden = false;
      el.cardPresentation.classList.remove('entering', 'turning', 'leaving');
      showCard(0);
      setPhase('revealing');
      el.drawButton.focus({ preventScroll: true });
      return;
    }
    el.cardPresentation.hidden = true;
    el.resultsGrid.replaceChildren();
    el.resultsGrid.classList.toggle('single', results.length === 1);
    results.forEach((card, index) => {
      const item = document.createElement('div');
      item.className = 'result-card'; item.dataset.rarity = card.rarity;
      item.style.setProperty('--i', index);
      const art = document.createElement('div'); art.className = 'result-art';
      const image = document.createElement('img'); image.src = cardImage(card); image.alt = card.name;
      const rarity = document.createElement('span'); rarity.className = 'result-rarity'; rarity.textContent = card.rarity;
      const name = document.createElement('span'); name.className = 'result-name'; name.textContent = card.name;
      art.append(image, rarity); item.append(art, name); el.resultsGrid.append(item);
    });
    const counts = results.reduce((acc, card) => { acc[card.rarity] = (acc[card.rarity] || 0) + 1; return acc; }, {});
    el.resultsCount.textContent = `${results.length} 张卡片`;
    el.resultsNote.textContent = ['SSR', 'SR', 'R'].filter(rarity => counts[rarity]).map(rarity => `${rarity} × ${counts[rarity]}`).join('   ·   ');
    el.stageStatus.textContent = '索隆卡片，全部揭晓';
    el.resultsPanel.hidden = false;
    el.resultsPanel.scrollTop = 0;
    setPhase('results');
    announce(`本次探索结束，共 ${results.length} 张卡片。${el.resultsNote.textContent}`);
    el.againButton.focus({ preventScroll: true });
  }

  function returnToPacks() {
    if (!loaded || isBusy()) return;
    ++runId;
    changingCard = false;
    lightAnimation?.cancel();
    el.cardPresentation.hidden = true;
    el.cardPresentation.classList.remove('entering', 'turning', 'leaving');
    el.resultsPanel.hidden = true;
    el.resultsGrid.replaceChildren();
    results = [];
    revealedIndex = 0;
    setPhase('idle');
    updateCount(selectedCount);
    announce('已返回卡包，可以再次开启或重播上次结果');
    el.drawButton.focus({ preventScroll: true });
  }

  async function preload() {
    setPhase('loading');
    el.drawLabel.textContent = '准备卡面中…';
    const sources = ['assets/zoro-pack-front-hd.svg', 'assets/zoro-pack-back-hd.svg', 'assets/zoro-card-back.svg', 'assets/zoro-card-art.png'];
    const prepared = new Map();
    const objectUrls = [];
    try {
      const checks = await Promise.allSettled(sources.map(async path => {
        let src = asset(path);
        // Local files and the self-contained build already have durable URLs.
        if (!assetSources.has(path) && /^https?:$/.test(location.protocol)) {
          const response = await fetch(path);
          if (!response.ok) throw new Error(`Missing ${path}`);
          src = URL.createObjectURL(await response.blob());
          objectUrls.push(src);
        }
        const image = new Image();
        await new Promise((resolve, reject) => {
          image.onload = resolve; image.onerror = () => reject(new Error(`Invalid ${path}`)); image.src = src;
        });
        // Decode before enabling draws, not on the first visible card turn.
        if (image.decode) await image.decode();
        prepared.set(path, src);
      }));
      if (checks.some(check => check.status === 'rejected')) throw new Error('Asset preload failed');
      prepared.forEach((src, path) => assetSources.set(path, src));
      // The initial scene is built before preload. Move its existing images to
      // the same durable URLs used by every subsequent animation phase.
      document.querySelectorAll('img').forEach(image => {
        const source = image.getAttribute('src');
        if (assetSources.has(source)) image.src = assetSources.get(source);
      });
      loaded = true;
      setPhase('idle'); updateCount(selectedCount);
      announce('卡面已就绪，可以开始探索');
    } catch {
      objectUrls.forEach(url => URL.revokeObjectURL(url));
      loaded = false;
      setPhase('error');
      el.stageStatus.textContent = '卡面未加载成功，请重试';
      announce('卡面加载失败，请点击重新加载卡面');
    }
  }

  quantityButtons.forEach(button => button.addEventListener('click', () => {
    if (!isBusy()) updateCount(Number(button.dataset.count));
  }));
  el.drawButton.addEventListener('click', () => {
    if (!loaded) void preload();
    else if (!isBusy()) void start(createDemoResult(selectedCount));
  });
  el.againButton.addEventListener('click', () => { if (!isBusy()) void start(createDemoResult(selectedCount)); });
  el.singleBackButton.addEventListener('click', returnToPacks);
  el.resultsBackButton.addEventListener('click', returnToPacks);
  el.replayButton.addEventListener('click', () => { if (previousResults.length && !isBusy()) void start(previousResults); });
  el.skipButton.addEventListener('click', () => { if (isBusy() && results.length) showResults(); });
  el.revealCard.addEventListener('click', advance);
  el.rotationToggle.addEventListener('click', () => {
    if (isBusy()) return;
    orbitRunning = !orbitRunning;
    el.rotationToggle.textContent = orbitRunning ? '暂停旋转' : '继续旋转';
    el.rotationToggle.setAttribute('aria-pressed', String(orbitRunning));
    syncMotion();
  });
  el.frontViewButton.addEventListener('click', () => {
    if (isBusy()) return;
    orbitRunning = false;
    el.rotationToggle.textContent = '继续旋转';
    el.rotationToggle.setAttribute('aria-pressed', 'false');
    syncMotion();
    seekOrbit(0);
  });
  el.stage.addEventListener('pointerdown', event => {
    if (phase !== 'idle' || el.stage.dataset.mode !== 'ten' || event.target.closest('button')) return;
    orbitDrag = { pointer: event.pointerId, x: event.clientX, angle: currentOrbitAngle() };
    syncMotion();
    el.stage.setPointerCapture(event.pointerId);
  });
  function finishOrbitDrag(event) {
    if (!orbitDrag || event.pointerId !== orbitDrag.pointer) return;
    if (el.stage.hasPointerCapture(event.pointerId)) el.stage.releasePointerCapture(event.pointerId);
    orbitDrag = null;
    syncMotion();
  }
  el.stage.addEventListener('pointerup', finishOrbitDrag);
  el.stage.addEventListener('pointercancel', finishOrbitDrag);
  el.soundToggle.addEventListener('click', async () => {
    soundEnabled = !soundEnabled;
    el.soundToggle.setAttribute('aria-pressed', String(soundEnabled));
    el.soundToggle.setAttribute('aria-label', soundEnabled ? '关闭音效' : '开启音效');
    el.soundLabel.textContent = soundEnabled ? '声音开启' : '声音关闭';
    if (soundEnabled) { await ensureAudio(); sound('tap'); }
  });
  el.stage.addEventListener('pointermove', event => {
    if (orbitDrag && event.pointerId === orbitDrag.pointer && phase === 'idle') {
      seekOrbit(orbitDrag.angle + (event.clientX - orbitDrag.x) * .25);
      return;
    }
    if (reducedMotion.matches || event.pointerType === 'touch') return;
    if (!((phase === 'idle' && el.stage.dataset.mode === 'single') || (phase === 'revealing' && !changingCard))) return;
    const box = el.stage.getBoundingClientRect();
    const x = (event.clientX - box.left) / box.width - .5;
    const y = (event.clientY - box.top) / box.height - .5;
    if (phase === 'idle') {
      el.stage.style.setProperty('--rotate-y', `${x * 65}deg`);
      el.stage.style.setProperty('--rotate-x', `${-y * 16}deg`);
      el.stage.style.setProperty('--sheen-x', `${x * 45}%`);
    }
    if (phase === 'revealing' && !changingCard) {
      el.revealCard.style.setProperty('--card-y', `${x * 13}deg`);
      el.revealCard.style.setProperty('--card-x', `${-y * 10}deg`);
      el.revealCard.style.setProperty('--card-shine', `${x * 70}%`);
    }
  });
  el.stage.addEventListener('pointerleave', () => {
    el.stage.style.setProperty('--rotate-y', '-15deg'); el.stage.style.setProperty('--rotate-x', '3deg');
    el.stage.style.setProperty('--fleet-y', '-5deg'); el.stage.style.setProperty('--fleet-x', '3deg');
    el.revealCard.style.setProperty('--card-y', '0deg'); el.revealCard.style.setProperty('--card-x', '0deg');
  });
  document.addEventListener('keydown', event => {
    if (event.key === 'Escape' && isBusy() && results.length) { event.preventDefault(); showResults(); }
  });

  // Native orbit animation, including its pause/seek state. In normal use no
  // requestAnimationFrame loop or periodic style updates are needed at all.
  function shouldRotate() {
    return !document.hidden && !reducedMotion.matches && !orbitDrag && orbitRunning &&
      el.stage.dataset.mode === 'ten' && isOrbitPhase();
  }
  function syncMotion() {
    if (!orbitAnimation) return;
    if (shouldRotate()) {
      if (orbitAnimation.playState !== 'running') orbitAnimation.play();
      el.stage.dataset.orbitState = 'running';
    } else {
      orbitAnimation.pause();
      orbitAngle = currentOrbitAngle();
      el.stage.dataset.orbitAngle = orbitAngle.toFixed(2);
      el.stage.dataset.orbitState = 'paused';
    }
  }
  new ResizeObserver(layoutFleet).observe(el.stage);
  document.addEventListener('visibilitychange', syncMotion);
  reducedMotion.addEventListener('change', syncMotion);
  buildFleet(); syncMotion(); void preload();
})();
