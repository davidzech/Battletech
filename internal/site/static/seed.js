// Seed popup. Step 1: choose or randomly generate a seed (the server sets the cookie).
// Step 2 (new campaigns only): paste the MekBay hangar link and pick the number of human players.
(function () {
  var dialog = document.getElementById('seed-dialog');
  if (!dialog || typeof dialog.showModal !== 'function') return;

  var seedForm = document.getElementById('seed-form');
  var setupForm = document.getElementById('setup-form');
  var input = document.getElementById('seed-input');
  var seedError = document.getElementById('seed-error');
  var hangarInput = document.getElementById('hangar-input');
  var setupError = document.getElementById('setup-error');

  var hasSeed = dialog.dataset.seed !== '';
  var configured = dialog.dataset.configured === '1';

  var SEED_PATTERN = /^[a-z0-9]{8,32}$/;
  var UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

  function skipped() { try { return sessionStorage.getItem('seedSkipped') === '1'; } catch (e) { return false; } }
  function remember() { try { sessionStorage.setItem('seedSkipped', '1'); } catch (e) {} }

  // Random seeds read like a callsign: two English words and three digits, e.g. "ironfalcon417".
  var ADJECTIVES = [
	'amber', 'ashen', 'azure', 'bitter', 'black', 'bleak', 'bold', 'brazen', 'bright', 'brisk',
  	'broken', 'bronze', 'burning', 'calm', 'cold', 'copper', 'crimson', 'crooked', 'dark', 'dawn',
  	'deep', 'distant', 'dread', 'dusky', 'eager', 'early', 'empty', 'faded', 'fallen', 'feral',
  	'fierce', 'final', 'fleet', 'frozen', 'gilded', 'golden', 'grand', 'gray', 'grim', 'hard',
  	'hidden', 'hollow', 'hungry', 'idle', 'iron', 'ivory', 'keen', 'lone', 'loyal', 'lucid',
  	'mighty', 'mute', 'narrow', 'noble', 'northern', 'oaken', 'odd', 'pale', 'patient', 'proud',
  	'quick', 'quiet', 'rapid', 'restless', 'rough', 'rugged', 'rusted', 'sacred', 'savage',
  	'scarlet', 'sharp', 'silent', 'silver', 'slate', 'sleek', 'slow', 'solemn', 'stark', 'steady',
  	'steel', 'stern', 'stoic', 'stormy', 'sturdy', 'swift', 'tame', 'tawny', 'thorned', 'true',
  	'vast', 'vivid', 'weary', 'wild', 'winter', 'wise', 'wry'
  ];
  var NOUNS = [
	'anvil', 'arrow', 'ash', 'badger', 'banner', 'basalt', 'beacon', 'blade', 'blaze', 'bolt',
  	'boulder', 'bridge', 'canyon', 'cinder', 'circle', 'citadel', 'cliff', 'comet', 'crag',
  	'crest', 'crown', 'dagger', 'delta', 'dune', 'eagle', 'ember', 'falcon', 'fang', 'flame',
  	'forge', 'fox', 'frost', 'gale', 'gauntlet', 'glacier', 'granite', 'hammer', 'harbor', 'hawk',
  	'helm', 'hollow', 'horizon', 'hound', 'jackal', 'keep', 'lance', 'lantern', 'lynx', 'mantle',
  	'marsh', 'mesa', 'mirror', 'moon', 'oath', 'onyx', 'outpost', 'owl', 'pact', 'peak', 'pike',
  	'plain', 'prowler', 'quarry', 'raven', 'reef', 'relic', 'ridge', 'ripple', 'rook', 'sabre',
  	'sand', 'shield', 'shrike', 'siege', 'smoke', 'spear', 'spire', 'stag', 'steppe', 'stone',
  	'storm', 'summit', 'talon', 'tempest', 'thorn', 'thunder', 'tide', 'torch', 'tower', 'trial',
  	'tundra', 'valley', 'vanguard', 'vigil', 'viper', 'warden', 'wolf', 'wraith', 'zenith'
  ];

  // Unbiased random integer in [0, n): reject values that would skew the modulo.
  function randomInt(n) {
    var limit = Math.floor(0x100000000 / n) * n;
    var buf = new Uint32Array(1);
    do { crypto.getRandomValues(buf); } while (buf[0] >= limit);
    return buf[0] % n;
  }

  function randomSeed() {
    var digits = String(randomInt(1000));
    while (digits.length < 3) digits = '0' + digits;
    return ADJECTIVES[randomInt(ADJECTIVES.length)] + NOUNS[randomInt(NOUNS.length)] + digits;
  }

  function sanitize() {
    var clean = input.value.toLowerCase().replace(/[^a-z0-9]/g, '').slice(0, 32);
    if (clean !== input.value) input.value = clean;
  }

  // Mirrors the server's check (the server is still the authority).
  function hangarProblem(raw) {
    var u;
    try { u = new URL(raw.trim()); } catch (e) { return 'Paste a MekBay hangar link, like https://mekbay.com/?instance=…'; }
    var host = u.hostname.toLowerCase();
    if (u.protocol !== 'https:' || (host !== 'mekbay.com' && host !== 'www.mekbay.com')) {
      return 'Paste a MekBay hangar link, like https://mekbay.com/?instance=…';
    }
    if (!UUID.test((u.searchParams.get('instance') || '').toLowerCase())) return 'That MekBay link has no valid instance id (?instance=…).';
    var sel = (u.searchParams.get('sel') || '').toLowerCase();
    if (sel && !UUID.test(sel)) return 'That MekBay link has an invalid selection id (&sel=…).';
    return '';
  }

  function showStep(step) {
    seedForm.hidden = step !== 'seed';
    setupForm.hidden = step !== 'setup';
    seedError.textContent = '';
    setupError.textContent = '';
    if (step === 'seed') { input.focus(); input.select(); } else { hangarInput.focus(); }
  }

  function openDialog(step) {
    if (!dialog.open) dialog.showModal();
    showStep(step);
  }

  function close() {
    if (!hasSeed || !configured) remember();
    dialog.close();
  }

  function post(url, body) {
    return fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body,
      credentials: 'same-origin'
    });
  }

  input.addEventListener('input', function () { sanitize(); seedError.textContent = ''; });
  hangarInput.addEventListener('input', function () { setupError.textContent = ''; });

  document.getElementById('seed-random').addEventListener('click', function () {
    input.value = randomSeed();
    seedError.textContent = '';
    input.focus();
  });

  document.getElementById('seed-copy').addEventListener('click', function () {
    var btn = this;
    function done(ok) {
      btn.textContent = ok ? 'Copied' : 'Press Ctrl+C';
      setTimeout(function () { btn.textContent = 'Copy'; }, 1500);
    }
    input.select();
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(input.value).then(function () { done(true); }, function () { done(false); });
    } else {
      done(false);
    }
  });

  seedForm.addEventListener('submit', function (e) {
    e.preventDefault();
    sanitize();
    if (!SEED_PATTERN.test(input.value)) {
      seedError.textContent = 'A seed is 8 to 32 lowercase letters and digits.';
      return;
    }
    post('/api/seed', 'seed=' + encodeURIComponent(input.value)).then(function (res) {
      if (!res.ok) return res.text().then(function (t) { seedError.textContent = t.trim() || 'Could not save the seed.'; });
      return res.json().then(function (data) {
        hasSeed = true;
        if (data.configured) { location.reload(); } else { configured = false; showStep('setup'); }
      });
    }, function () { seedError.textContent = 'Could not reach the server.'; });
  });

  setupForm.addEventListener('submit', function (e) {
    e.preventDefault();
    var problem = hangarProblem(hangarInput.value);
    if (problem) { setupError.textContent = problem; return; }
    var players = setupForm.querySelector('input[name="players"]:checked');
    post('/api/campaign', 'hangar=' + encodeURIComponent(hangarInput.value.trim()) + '&players=' + encodeURIComponent(players ? players.value : ''))
      .then(function (res) {
        if (res.ok) { location.reload(); return; }
        return res.text().then(function (t) { setupError.textContent = t.trim() || 'Could not save the campaign.'; });
      }, function () { setupError.textContent = 'Could not reach the server.'; });
  });

  document.getElementById('seed-cancel').addEventListener('click', close);
  document.getElementById('setup-cancel').addEventListener('click', close);
  document.getElementById('setup-back').addEventListener('click', function () { showStep('seed'); });
  var openSetup = document.getElementById('open-setup');
  if (openSetup) openSetup.addEventListener('click', function () { showStep('setup'); });
  dialog.addEventListener('cancel', function () { if (!hasSeed || !configured) remember(); }); // Esc key

  document.getElementById('seed-btn').addEventListener('click', function () {
    openDialog(hasSeed && !configured ? 'setup' : 'seed');
  });

  if (!skipped()) {
    if (!hasSeed) openDialog('seed');
    else if (!configured) openDialog('setup');
  }
})();
