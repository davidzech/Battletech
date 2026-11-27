// Shared set-up for the tests: a fresh account per test, made through the site's own endpoints (page.request shares the page's cookies), and the fake
// MekBay's hangar and battle set through the test server's /__test/ endpoints (internal/site/browser_test.go).
const base = require('@playwright/test');

// The test fixture fails a test on any uncaught error in a page script, which no assertion would otherwise notice.
const test = base.test.extend({
  page: async ({ page }, use) => {
    const errors = [];
    page.on('pageerror', (e) => errors.push(e.message));
    await use(page);
    base.expect(errors, 'uncaught errors in the page scripts').toEqual([]);
  }
});
const expect = base.expect;

// A hangar link the fake MekBay accepts (any instance will do: it answers the same hangar for all).
const HANGAR_LINK = 'https://mekbay.com/?instance=01a12f9e-188f-7158-ad67-3fc9700ec733';

let seq = 0;
function freshSeed() {
  seq++;
  return ('bt' + Date.now().toString(36) + seq.toString(36) + Math.random().toString(36).slice(2, 8)).slice(0, 32);
}

async function ok(res, what) {
  if (!res.ok()) throw new Error(what + ': ' + res.status() + ' ' + (await res.text()));
  return res;
}

// A new seed with a named account, set as the page's cookie.
async function newAccount(page, name) {
  await ok(await page.request.post('/api/seed', { form: { seed: freshSeed() } }), 'seed');
  await ok(await page.request.post('/api/account', { form: { account: name || 'Wolf Pack' } }), 'account');
}

// A hangar unit as the fake MekBay answers it (mekbay.HangarUnit's fields).
function unit(id, unitId, extra) {
  return Object.assign({ ID: id, Unit: unitId, Group: 'Hangar', Gunnery: 4, Piloting: 5 }, extra || {});
}

// The hangar every read of the fake MekBay answers from now on.
async function setHangar(page, units) {
  await ok(await page.request.post('/__test/hangar', { data: { Name: 'Test Hangar', Timestamp: 't', Units: units } }), 'set hangar');
}

// The battle status the fake MekBay reports (mekbay.BattleStatus).
async function setBattle(page, status) {
  await ok(await page.request.post('/__test/battle', { data: status }), 'set battle');
}

// Five mechs under 10,000 BV: meets Refusal War's starting conditions.
const STARTING = [unit('ua', 'BMKoshi_E'), unit('ub', 'BMUller_J'), unit('uc', 'BMDasher_Prime'), unit('ud', 'BMGrizzly'), unit('ue', 'BMLoki_Prime')];

// Starts a campaign through the endpoint the start wizard posts to, from the fake hangar, with a named pilot per name (90 SP to Gunnery, 60 to Edge
// tokens).
async function startCampaign(page, campaign, names, units) {
  await setHangar(page, units || STARTING);
  const form = { campaign: campaign, hangar: HANGAR_LINK, players: '1', force_name: 'Wolf Spiders' };
  names.forEach((n, i) => {
    form['pilot' + i + '_name'] = n;
    form['pilot' + i + '_gunnery'] = '90';
    form['pilot' + i + '_edge_tokens'] = '60';
  });
  await ok(await page.request.post('/api/campaign/start', { form: form }), 'start');
}

// Sets the warchest by hand (Advanced Management), for tests about prices.
async function setWarchest(page, campaign, sp) {
  await ok(await page.request.post('/api/warchest', { form: { campaign: campaign, funds: String(sp) } }), 'warchest');
}

// Recruits a named pilot (from the warchest), 90 SP to Gunnery and 60 to Edge tokens.
async function recruit(page, campaign, name) {
  await ok(await page.request.post('/api/pilots', { form: { campaign: campaign, name: name, gunnery: '90', edge_tokens: '60' } }), 'recruit');
}

module.exports = { test, expect, newAccount, unit, setHangar, setBattle, startCampaign, setWarchest, recruit, STARTING, HANGAR_LINK };
