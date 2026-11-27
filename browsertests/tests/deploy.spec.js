// The Deploy wizard (mekbay.js) on Refusal War's Trial of Position: the scenario provides its own omnimechs (two at most, each with a named pilot) and lets
// them be reconfigured for free, so the wizard has the pilot tray, the unit limit, the Configure step and Start battle; then the Operation in progress view,
// the Complete mission dialog reading the battle (outcome.js) and Abort mission.
const { test, expect, newAccount, startCampaign, setBattle, setHangar, setWarchest, recruit, unit } = require('./helpers');

const SCENARIO = '/refusalwar/scenario_000/';

// The battle the fake MekBay reports: Alrek and Katarina each crew a mech, and Katarina's record sheet shows her dead.
async function battleWithLosses(page) {
  await setBattle(page, {
    Forces: [{
      InstanceID: 'p1', Alignment: 'friendly',
      Force: {
        Name: 'Player 1', Units: [
          { ID: 'b1', Unit: 'BMBlackHawk_Prime', Group: 'Deployed', Pilot: 'Alrek', Gunnery: 3, Piloting: 5 },
          { ID: 'b2', Unit: 'BMRyoken_Prime', Group: 'Deployed', Pilot: 'Katarina', Gunnery: 3, Piloting: 5, CrewDead: true, Damaged: true }
        ]
      }
    }]
  });
}

async function openDeploy(page) {
  await page.goto(SCENARIO);
  await page.locator('#deploy-open').click();
  const dlg = page.locator('#deploy-dialog');
  await expect(dlg).toBeVisible();
  await expect(dlg.locator('#deploy-list .deploy-unit')).toHaveCount(5);
  return dlg;
}

// Ticking a mech updates the list in place: the other rows (their icons and pilot portraits) are the same elements, so nothing flashes, while the limit
// and the slots still follow the ticks.
test('ticking a mech keeps the other rows', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina']);
  const dlg = await openDeploy(page);
  const rows = dlg.locator('#deploy-list .deploy-unit');
  const tray = dlg.locator('#deploy-pilots-list .deploy-pilot');
  const box = (i) => rows.nth(i).locator('input[type=checkbox]');
  // Crew the first mech, then mark every icon and portrait in the list.
  await tray.filter({ hasText: 'Alrek' }).click();
  await rows.nth(0).locator('.deploy-slot').click();
  await expect(rows.nth(0).locator('.deploy-slot')).toContainText('Alrek');
  await dlg.locator('#deploy-list').evaluate((list) => list.querySelectorAll('img').forEach((img) => { img.dataset.kept = '1'; }));
  const kept = () => dlg.locator('#deploy-list').evaluate((list) => [...list.querySelectorAll('img')].map((img) => img.dataset.kept === '1'));
  const marked = await kept();
  expect(marked.length).toBeGreaterThan(5);

  // Taking a second mech reaches the limit of two: the others can't be taken and say so, and no image was rebuilt.
  await box(1).check();
  await expect(rows.nth(1)).toHaveClass(/picked/);
  await expect(box(2)).toBeDisabled();
  await expect(rows.nth(3).locator('.deploy-slot')).toContainText('Mech limit reached');
  await expect(dlg.locator('#deploy-meter')).toContainText('2 / 2 mechs');
  expect(await kept()).toEqual(marked);

  // Putting the crewed mech back frees its pilot and the limit; only its own slot (with Alrek's portrait) is rebuilt.
  await box(0).uncheck();
  await expect(rows.nth(0)).not.toHaveClass(/picked/);
  await expect(rows.nth(0).locator('.deploy-slot')).not.toContainText('Alrek');
  await expect(tray.filter({ hasText: 'Alrek' })).not.toContainText('in ');
  await expect(box(2)).toBeEnabled();
  await expect(rows.nth(3).locator('.deploy-slot')).not.toContainText('Mech limit reached');
  await expect(dlg.locator('#deploy-meter')).toContainText('1 / 2 mechs');
  const icons = await dlg.locator('#deploy-list img.mr-icon').evaluateAll((imgs) => imgs.map((img) => img.dataset.kept === '1'));
  expect(icons).toEqual(icons.map(() => true));

  // Placing a pilot still redraws the list: an icon the page has already loaded comes back drawn at once, not lazily (which showed it blank for a frame).
  await tray.filter({ hasText: 'Katarina' }).click();
  await rows.nth(1).locator('.deploy-slot').click();
  await expect(rows.nth(1).locator('.deploy-slot')).toContainText('Katarina');
  await expect(rows.nth(1).locator('img.mr-icon')).not.toHaveAttribute('data-kept', '1');
  await expect(rows.nth(1).locator('img.mr-icon')).not.toHaveAttribute('loading', 'lazy');
});

test('deploys two pilots, reconfigures a mech, starts the battle and completes the mission', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina']);
  await battleWithLosses(page);
  const dlg = await openDeploy(page);
  const rows = dlg.locator('#deploy-list .deploy-unit');
  const tray = dlg.locator('#deploy-pilots-list .deploy-pilot');
  await expect(tray).toHaveCount(2);
  await expect(dlg.locator('#deploy-rules')).toContainText('Take up to 2 mechs in all, every mech needs a named pilot.');
  await expect(dlg.locator('#deploy-next')).toBeDisabled();

  // Click a pilot, then a mech's slot: the pilot goes in and the mech is taken.
  await tray.filter({ hasText: 'Alrek' }).click();
  await expect(tray.filter({ hasText: 'Alrek' })).toHaveAttribute('aria-pressed', 'true');
  await rows.nth(0).locator('.deploy-slot').click();
  await expect(rows.nth(0).locator('input[type=checkbox]')).toBeChecked();
  await expect(rows.nth(0).locator('.deploy-slot')).toContainText('Alrek');
  await expect(tray.filter({ hasText: 'Alrek' })).toContainText('in ');

  // Drag the other pilot onto a second mech.
  await tray.filter({ hasText: 'Katarina' }).dragTo(rows.nth(1).locator('.deploy-slot'));
  await expect(rows.nth(1).locator('.deploy-slot')).toContainText('Katarina');
  await expect(rows.nth(1).locator('input[type=checkbox]')).toBeChecked();

  // Two mechs is the scenario's limit: the rest can't be taken.
  await expect(rows.nth(2).locator('input[type=checkbox]')).toBeDisabled();
  await expect(dlg.locator('#deploy-meter')).toContainText('2 / 2 mechs');

  // The x on a slot takes the pilot back out; a mech without a pilot holds Next back.
  await rows.nth(1).getByRole('button', { name: /Take Katarina out/ }).click();
  await expect(rows.nth(1).locator('.deploy-slot')).not.toContainText('Katarina');
  await expect(dlg.locator('#deploy-next')).toBeDisabled();
  await tray.filter({ hasText: 'Katarina' }).click();
  await rows.nth(1).locator('.deploy-slot').click();
  await expect(dlg.locator('#deploy-next')).toBeEnabled();
  await dlg.locator('#deploy-next').click();

  // The review: each omnimech taken has a Reconfigure button, free for the Trial's own mechs, and with no drop cost.
  await expect(dlg.locator('#deploy-step')).toHaveText('Review your plan');
  const reviewed = dlg.locator('.deploy-review .deploy-unit');
  await expect(reviewed).toHaveCount(2);
  await reviewed.first().getByRole('button', { name: 'Reconfigure' }).click();
  const swap = page.locator('#swap-dialog');
  await expect(swap).toBeVisible();
  await expect(swap.locator('#swap-available')).toBeHidden(); // only the variants Clan Wolf fields, without a box
  const options = await swap.locator('#swap-select option').evaluateAll((os) => os.map((o) => o.value).filter(Boolean));
  expect(options.length).toBeGreaterThan(0);
  await swap.locator('#swap-select').selectOption(options[0]);
  await expect(swap.locator('.swap-flow')).toContainText('Free');
  await expect(swap.locator('#swap-breakdown')).toBeHidden();
  await swap.locator('#swap-apply').click();
  await expect(swap).toBeHidden();
  await expect(reviewed.first()).toContainText('Prime → ');
  await expect(reviewed.first().getByRole('button', { name: 'Reconfigured' })).toBeVisible();
  await expect(dlg.locator('.deploy-drop-cost')).toHaveCount(0);

  await expect(dlg.locator('#deploy-go')).toBeEnabled();
  await dlg.locator('#deploy-go').click();

  // The Operation in progress view: the table's link and the battle as MekBay has it, with Katarina dead on her record sheet.
  await expect(dlg.locator('#deploy-title')).toHaveText('Operation in progress');
  await expect(dlg.locator('.battle-link-url')).toContainText('mekbay-');
  await expect(dlg.locator('.op-force')).toHaveCount(1);
  await expect(dlg.locator('.op-force .deploy-unit')).toHaveCount(2);
  await expect(dlg.locator('.op-force .kia-stamp')).toHaveCount(1);

  // Complete mission: the dialog reads who took part and who died from the battle.
  await dlg.locator('#outcome-open').click();
  const out = page.locator('#outcome-dialog');
  await expect(out).toBeVisible();
  const alrek = out.locator('.outcome-pilot', { hasText: 'Alrek' });
  const katarina = out.locator('.outcome-pilot', { hasText: 'Katarina' });
  await expect(alrek.locator('.outcome-took')).toHaveText('Took part');
  await expect(katarina.locator('.outcome-took')).toContainText('dead on the record sheet');
  await expect(katarina.getByLabel('Killed in action')).toBeChecked();
  await expect(alrek.getByLabel('Killed in action')).not.toBeChecked();
  await alrek.getByLabel('Kills').fill('2');
  await katarina.getByLabel('Kills').fill('1');
  // Nothing can be committed until the MVP is chosen; only Alrek, alive, can be it.
  await expect(out.locator('.outcome-mvp-needed')).toBeVisible();
  await expect(out.locator('#outcome-commit')).toBeDisabled();
  await expect(out.locator('#outcome-mvp option:not([disabled])')).toHaveText(['Alrek']);
  await out.locator('#outcome-mvp').selectOption({ label: 'Alrek' });
  await expect(alrek.locator('.outcome-preview')).toContainText('400');
  await expect(katarina.locator('.outcome-preview')).toContainText('Killed in Action');
  await expect(out.locator('#outcome-commit')).toBeEnabled();
  await out.locator('#outcome-commit').click();

  // Committing asks first (confirm.js), then the page reloads with the mission completed.
  const confirm = page.getByRole('dialog', { name: 'Commit this outcome?' });
  await expect(confirm).toBeVisible();
  await confirm.getByRole('button', { name: 'Commit' }).click();
  await expect(page.locator('#mission-log-state')).toHaveText('Completed');
  await expect(page.locator('#outcome-summary')).toContainText('Alrek');
  await expect(page.locator('#outcome-summary')).toContainText('Killed in Action');
});

test('aborts a battle and rolls the campaign back', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina']);
  // A recruit after the start, which the rollback undoes.
  await recruit(page, 'refusalwar', 'Jarek');
  await battleWithLosses(page);
  const dlg = await openDeploy(page);
  const rows = dlg.locator('#deploy-list .deploy-unit');
  // Ticking a mech's box takes it; the slot then waits for its pilot.
  await rows.nth(0).locator('input[type=checkbox]').check();
  await expect(rows.nth(0)).toHaveClass(/picked/);
  await expect(dlg.locator('#deploy-rules')).toContainText('Assign a named pilot to each mech you take.');
  await dlg.locator('#deploy-pilots-list .deploy-pilot', { hasText: 'Alrek' }).dragTo(rows.nth(0).locator('.deploy-slot'));
  await dlg.locator('#deploy-next').click();
  // One player also reviews the plan before Start battle.
  await expect(dlg.locator('#deploy-step')).toHaveText('Review your plan');
  await expect(dlg.locator('.deploy-review-player h3')).toContainText('Your force');
  await dlg.locator('#deploy-go').click();
  await expect(dlg.locator('#deploy-title')).toHaveText('Operation in progress');

  // Abort mission is under More options; the choice is its own dialog.
  await dlg.locator('#op-abort summary').click();
  await dlg.locator('#abort-open').click();
  const abort = page.getByRole('dialog', { name: 'Abort mission' });
  await expect(abort).toBeVisible();
  await abort.getByRole('button', { name: 'Keep playing' }).click();
  await expect(abort).toBeHidden();
  await expect(dlg.locator('#deploy-title')).toHaveText('Operation in progress');
  await dlg.locator('#abort-open').click();
  const reloaded = page.waitForEvent('load');
  await abort.getByRole('button', { name: 'Roll back' }).click();

  // The page reloads with the mission not started again.
  await reloaded;
  await expect(page.locator('#deploy-open')).toHaveText('Deploy force');
  await expect(page.locator('#mission-log-state')).toHaveText('Not completed');
  await page.goto('/refusalwar/');
  await expect(page.locator('.roster-pilot')).toHaveText([/Alrek/, /Katarina/]);
});

test('divides the force between two players and reviews the plan', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina']);
  await battleWithLosses(page);
  const dlg = await openDeploy(page);
  const pick = dlg.locator('#deploy-players-pick');
  await expect(pick.getByRole('button', { name: '1 player', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await pick.getByRole('button', { name: '2 players' }).click();
  await expect(pick.getByRole('button', { name: '2 players' })).toHaveAttribute('aria-pressed', 'true');
  await expect(dlg.locator('#deploy-step')).toHaveText('Player 1 of 2');

  const rows = dlg.locator('#deploy-list .deploy-unit');
  const tray = dlg.locator('#deploy-pilots-list .deploy-pilot');
  await tray.filter({ hasText: 'Alrek' }).click();
  await rows.nth(0).locator('.deploy-slot').click();
  // With two players and a limit of two mechs, Player 1 leaves one for Player 2.
  await expect(rows.nth(1).locator('input[type=checkbox]')).toBeDisabled();
  await dlg.locator('#deploy-next').click();

  // Player 2 sees neither the mech nor the pilot Player 1 took.
  await expect(dlg.locator('#deploy-step')).toHaveText('Player 2 of 2');
  await expect(rows).toHaveCount(4);
  await expect(tray).toHaveCount(1);
  await tray.click();
  await rows.nth(0).locator('.deploy-slot').click();
  await dlg.locator('#deploy-next').click();

  await expect(dlg.locator('#deploy-step')).toHaveText('Review your plan');
  await expect(dlg.locator('.deploy-review-player')).toHaveCount(2);
  await expect(dlg.locator('.deploy-review-player').nth(0)).toContainText('Alrek');
  await expect(dlg.locator('.deploy-review-player').nth(1)).toContainText('Katarina');
  await expect(dlg.locator('#deploy-count')).toHaveText('Step 3 of 3');

  // The review's rows show their unit cards on hover.
  await dlg.locator('.deploy-review .mech-row').first().locator('.mr-name').hover();
  await expect(dlg.locator('#pill-tip .tip-head')).toBeVisible();

  await dlg.locator('#deploy-go').click();
  await expect(dlg.locator('#deploy-title')).toHaveText('Operation in progress');
  await dlg.locator('.op-force .mech-row').first().locator('.mr-name').hover();
  await expect(dlg.locator('#pill-tip .tip-head')).toBeVisible();
});

// A force over the BV limit can go on to the review, where reconfiguring a hangar omnimech brings it under: the review adds up the drop cost, and Start battle
// waits until the force fits and the warchest can pay. Muster's Scenario 000 (a test campaign) is deployed from the hangar, with a 7,500 BV limit.
test('reconfigures an omnimech on the review to fit the BV limit', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'muster', ['Alrek', 'Katarina']);
  // Two Timber Wolf Primes (2,737 BV) and two Fire Moth Primes (1,251): 7,976 BV. The Timber Wolf B (2,224 BV, 38 SP) brings it to 7,463. The fifth mech,
  // a Timber Wolf with spent ammunition, can't be reconfigured.
  await setHangar(page, [unit('ua', 'BMMadCat_Prime'), unit('ub', 'BMMadCat_Prime'), unit('uc', 'BMDasher_Prime'), unit('ud', 'BMDasher_Prime'),
    unit('ue', 'BMMadCat_Prime', { Damaged: true, AmmoBins: 1, AmmoUsed: 3 })], 'muster');
  await setWarchest(page, 'muster', 1000);
  await page.goto('/muster/scenario_000/');
  await page.locator('#deploy-open').click();
  const dlg = page.locator('#deploy-dialog');
  const rows = dlg.locator('#deploy-list .deploy-unit');
  await expect(rows).toHaveCount(5);
  for (let i = 0; i < 4; i++) await rows.nth(i).locator('input[type=checkbox]').check();

  // Over the limit, the step says so but lets the force go on.
  await expect(dlg.locator('#deploy-meter')).toHaveClass(/over/);
  await expect(dlg.locator('#deploy-rules')).toContainText('over the BV limit');
  await expect(dlg.locator('#deploy-next')).toBeEnabled();
  await dlg.locator('#deploy-next').click();
  await expect(dlg.locator('#deploy-step')).toHaveText('Review your plan');
  // The totals come first, so what holds Start battle back is in sight without scrolling.
  await expect(dlg.locator('#deploy-review > *').first()).toHaveClass(/deploy-review-total/);
  await expect(dlg.locator('.deploy-review-total').first()).toHaveClass(/over/);
  await expect(dlg.locator('.deploy-review-total').first()).toContainText('7,976 / 7,500 BV');
  // The BV bar under the total: full and red over the limit.
  await expect(dlg.locator('.deploy-review-total .bv-bar').first()).toHaveClass(/over/);
  await expect(dlg.locator('.deploy-review-total .bv-bar').first()).toHaveAttribute('aria-valuenow', '7976');
  await expect(dlg.locator('#deploy-go')).toBeDisabled();

  // Reconfigure the first Timber Wolf to the B: the Manage page's dialog, with its price, breakdown and the box for the faction's variants.
  const reviewed = dlg.locator('.deploy-review .deploy-unit');
  await reviewed.first().getByRole('button', { name: 'Reconfigure' }).click();
  const swap = page.locator('#swap-dialog');
  await expect(swap.locator('#swap-title')).toHaveText('Reconfigure Timber Wolf');
  await expect(swap.locator('#swap-apply')).toBeDisabled();
  await swap.locator('#swap-available').uncheck();
  await swap.locator('#swap-select').selectOption('BMMadCat_B');
  await expect(swap.locator('#swap-cost')).toHaveText('Cost 38 SP. Warchest 1,000 → 962 SP.');
  await expect(swap.locator('#swap-breakdown')).toContainText('50% of tonnage');
  await swap.locator('#swap-apply').click();
  await expect(swap).toBeHidden();

  // The force fits now; the drop cost is what Start battle will pay.
  await expect(reviewed.first()).toContainText('Prime → B');
  await expect(dlg.locator('.deploy-review-total').first()).toContainText('7,463 / 7,500 BV');
  await expect(dlg.locator('.deploy-review-total .bv-bar').first()).not.toHaveClass(/over/);
  await expect(dlg.locator('.deploy-review-total .bv-bar').first()).toHaveAttribute('aria-valuenow', '7463');
  await expect(dlg.locator('.deploy-drop-cost')).toHaveText('Drop cost: Cost 38 SP. Warchest 1,000 → 962 SP.');
  await expect(dlg.locator('#deploy-go')).toBeEnabled();

  // Choosing the current variant again takes the proposal back.
  await reviewed.first().getByRole('button', { name: 'Reconfigured' }).click();
  await expect(swap.locator('#swap-select')).toHaveValue('BMMadCat_B');
  await swap.locator('#swap-select').selectOption('');
  await swap.locator('#swap-apply').click();
  await expect(dlg.locator('.deploy-drop-cost')).toHaveCount(0);
  await expect(dlg.locator('#deploy-go')).toBeDisabled();
  await reviewed.first().getByRole('button', { name: 'Reconfigure' }).click();
  await swap.locator('#swap-available').uncheck();
  await swap.locator('#swap-select').selectOption('BMMadCat_B');
  await swap.locator('#swap-apply').click();

  // Start battle pays for the swap, then builds the battle; the warchest shows it once the page reloads.
  await dlg.locator('#deploy-go').click();
  await expect(dlg.locator('#deploy-title')).toHaveText('Operation in progress');
  await expect(page).not.toHaveURL(/#deploy=/); // a battle started: nothing left to come back to
  await page.goto('/muster/manage/');
  await expect(page.locator('#overview')).toContainText('962 SP');
});

// A hangar omnimech that isn't fully repaired and rearmed can't be reconfigured on the review, as on the Manage page.
test('a damaged omnimech cannot be reconfigured on the review', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'muster', ['Alrek', 'Katarina']);
  await setHangar(page, [unit('ua', 'BMMadCat_Prime', { Damaged: true, AmmoBins: 1, AmmoUsed: 3 }), unit('ub', 'BMGrizzly')], 'muster');
  await page.goto('/muster/scenario_000/');
  await page.locator('#deploy-open').click();
  const dlg = page.locator('#deploy-dialog');
  await dlg.locator('#deploy-list .deploy-unit').nth(0).locator('input[type=checkbox]').check();
  await dlg.locator('#deploy-list .deploy-unit').nth(1).locator('input[type=checkbox]').check();
  await dlg.locator('#deploy-next').click();
  const reviewed = dlg.locator('.deploy-review .deploy-unit');
  await expect(reviewed.first().getByRole('button', { name: 'Reconfigure' })).toBeDisabled();
  await expect(reviewed.first().getByRole('button', { name: 'Reconfigure' })).toHaveAttribute('title', /fully repaired and rearmed/);
  await expect(reviewed.nth(1).getByRole('button', { name: 'Reconfigure' })).toHaveCount(0); // the Grizzly isn't an omnimech
});

// The mech steps offer Reconfigure too: a click on it doesn't tick or untick the mech, and choosing a variant for a mech not taken yet takes it, with the
// meter following.
test('reconfigures an omnimech on the mech step', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'muster', ['Alrek', 'Katarina']);
  await setHangar(page, [unit('ua', 'BMMadCat_Prime'), unit('ub', 'BMMadCat_Prime'), unit('uc', 'BMDasher_Prime'), unit('ud', 'BMDasher_Prime')], 'muster');
  await setWarchest(page, 'muster', 1000);
  await page.goto('/muster/scenario_000/');
  await page.locator('#deploy-open').click();
  const dlg = page.locator('#deploy-dialog');
  const rows = dlg.locator('#deploy-list .deploy-unit');
  const box = (i) => rows.nth(i).locator('input[type=checkbox]');
  for (let i = 1; i < 4; i++) await box(i).check();
  await expect(dlg.locator('#deploy-meter')).toContainText('3 selected · 5,239 / 7,500 BV');

  // Opening the dialog from a taken mech and cancelling leaves it taken.
  const swap = page.locator('#swap-dialog');
  await rows.nth(1).getByRole('button', { name: 'Reconfigure' }).click();
  await expect(swap).toBeVisible();
  await swap.locator('#swap-cancel').click();
  await expect(box(1)).toBeChecked();

  // Choosing the B for the first Timber Wolf, not taken yet, takes it as the B.
  await expect(box(0)).not.toBeChecked();
  await rows.nth(0).getByRole('button', { name: 'Reconfigure' }).click();
  await swap.locator('#swap-available').uncheck();
  await swap.locator('#swap-select').selectOption('BMMadCat_B');
  await swap.locator('#swap-apply').click();
  await expect(box(0)).toBeChecked();
  await expect(rows.nth(0)).toContainText('Prime → B');
  await expect(rows.nth(0).getByRole('button', { name: 'Reconfigured' })).toBeVisible();
  await expect(dlg.locator('#deploy-meter')).toContainText('4 selected · 7,463 / 7,500 BV');
  await expect(dlg.locator('#deploy-meter .bv-bar')).toHaveAttribute('aria-valuemax', '7500');
  await expect(dlg.locator('#deploy-meter')).not.toHaveClass(/over/);

  // The review carries it, with its drop cost, and Start battle can go.
  await dlg.locator('#deploy-next').click();
  await expect(dlg.locator('.deploy-drop-cost')).toHaveText('Drop cost: Cost 38 SP. Warchest 1,000 → 962 SP.');
  await expect(dlg.locator('#deploy-go')).toBeEnabled();
});

// With several players, a player who leaves no mech for a player after them can't go on: Next waits, and the step says why.
test('a player must leave a mech for each player after them', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'muster', ['Alrek', 'Katarina']);
  await setHangar(page, [unit('ua', 'BMMadCat_Prime'), unit('ub', 'BMDasher_Prime'), unit('uc', 'BMGrizzly', { Destroyed: true })], 'muster');
  await page.goto('/muster/scenario_000/');
  await page.locator('#deploy-open').click();
  const dlg = page.locator('#deploy-dialog');
  await dlg.locator('#deploy-players-pick').getByRole('button', { name: '2 players' }).click();
  const rows = dlg.locator('#deploy-list .deploy-unit');
  await rows.nth(0).locator('input[type=checkbox]').check();
  await rows.nth(1).locator('input[type=checkbox]').check();
  // The destroyed Grizzly is no mech to take, so Player 2 would have nothing.
  await expect(dlg.locator('#deploy-next')).toBeDisabled();
  await expect(dlg.locator('#deploy-rules')).toContainText('Leave a mech for each other player.');
  await rows.nth(1).locator('input[type=checkbox]').uncheck();
  await expect(dlg.locator('#deploy-next')).toBeEnabled();
  await expect(dlg.locator('#deploy-rules')).toBeHidden();
  await dlg.locator('#deploy-next').click();
  await expect(dlg.locator('#deploy-step')).toHaveText('Player 2 of 2');
});

// A row's buttons look the same in the Deploy wizard (a dialog, with buttons of its own) as on the Manage page.
test('the Reconfigure button looks the same in the wizard as on the Manage page', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'muster', ['Alrek', 'Katarina']);
  await setHangar(page, [unit('ua', 'BMMadCat_Prime')], 'muster');
  const look = (b) => b.evaluate((e) => {
    const c = getComputedStyle(e);
    return ['font-size', 'font-weight', 'letter-spacing', 'color', 'background-color', 'border', 'padding'].map((p) => c.getPropertyValue(p)).join(' | ');
  });
  await page.goto('/muster/manage/');
  await page.waitForLoadState('networkidle');
  const manage = await look(page.locator('#hangar-units-list').getByRole('button', { name: 'Reconfigure' }).first());
  await page.goto('/muster/scenario_000/');
  await page.locator('#deploy-open').click();
  await page.waitForLoadState('networkidle');
  await expect(page.locator('#deploy-list').getByRole('button', { name: 'Reconfigure' }).first()).toBeVisible();
  expect(await look(page.locator('#deploy-list').getByRole('button', { name: 'Reconfigure' }).first())).toEqual(manage);
});

// Dragging a pilot onto a mech doesn't take the meter's red off while the force is over the BV limit (the drag's clean-up once cleared every .over).
test('the meter stays red over the limit after dragging a pilot', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'muster', ['Alrek', 'Katarina']);
  await setHangar(page, [unit('ua', 'BMMadCat_Prime'), unit('ub', 'BMMadCat_Prime'), unit('uc', 'BMMadCat_Prime')], 'muster');
  await page.goto('/muster/scenario_000/');
  await page.locator('#deploy-open').click();
  const dlg = page.locator('#deploy-dialog');
  const rows = dlg.locator('#deploy-list .deploy-unit');
  for (let i = 0; i < 3; i++) await rows.nth(i).locator('input[type=checkbox]').check();
  await expect(dlg.locator('#deploy-meter')).toHaveClass(/over/);
  await dlg.locator('#deploy-pilots-list .deploy-pilot', { hasText: 'Alrek' }).dragTo(rows.nth(0).locator('.deploy-slot'));
  await expect(rows.nth(0).locator('.deploy-slot')).toContainText('Alrek');
  await expect(dlg.locator('#deploy-meter')).toHaveClass(/over/);
});

// The wizard opens with the keyboard on its title, not on its first button (the Players picker), so no control shows a focus ring the player didn't put there.
test('the wizard opens on its title', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'refusalwar', ['Alrek', 'Katarina']);
  await openDeploy(page);
  await expect(page.locator('#deploy-title')).toBeFocused();
});

// The wizard's plan is in the URL's fragment, so a reload brings the player back where they were; closing the wizard clears it, and a restore keeps only what
// still holds (a mech no longer in the hangar is dropped, with its pilot).
test('a reload brings the wizard back where it was', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'muster', ['Alrek', 'Katarina']);
  const hangar = [unit('ua', 'BMMadCat_Prime'), unit('ub', 'BMDasher_Prime'), unit('uc', 'BMGrizzly')];
  await setHangar(page, hangar, 'muster');
  await page.goto('/muster/scenario_000/');
  await page.locator('#deploy-open').click();
  const dlg = page.locator('#deploy-dialog');
  const rows = dlg.locator('#deploy-list .deploy-unit');
  await dlg.locator('#deploy-players-pick').getByRole('button', { name: '2 players' }).click();
  // Player 1: the Timber Wolf as the B, with Alrek.
  await rows.nth(0).getByRole('button', { name: 'Reconfigure' }).click();
  const swap = page.locator('#swap-dialog');
  await swap.locator('#swap-available').uncheck();
  await swap.locator('#swap-select').selectOption('BMMadCat_B');
  await swap.locator('#swap-apply').click();
  await dlg.locator('#deploy-pilots-list .deploy-pilot', { hasText: 'Alrek' }).click();
  await rows.nth(0).locator('.deploy-slot').click();
  await dlg.locator('#deploy-next').click();
  // Player 2: the Fire Moth.
  await expect(dlg.locator('#deploy-step')).toHaveText('Player 2 of 2');
  await rows.filter({ hasText: 'Fire Moth' }).locator('input[type=checkbox]').check();
  await expect(page).toHaveURL(/#deploy=/);

  await page.reload();
  await expect(dlg).toBeVisible();
  await expect(dlg.locator('#deploy-step')).toHaveText('Player 2 of 2');
  await expect(rows.filter({ hasText: 'Fire Moth' }).locator('input[type=checkbox]')).toBeChecked();
  await dlg.locator('#deploy-back').click();
  await expect(rows.nth(0)).toContainText('Prime → B');
  await expect(rows.nth(0).locator('.deploy-slot')).toContainText('Alrek');

  // A mech gone from the hangar is dropped on restore, and its pilot with it.
  await setHangar(page, hangar.slice(1), 'muster');
  await page.reload();
  await expect(dlg).toBeVisible();
  await expect(dlg.locator('#deploy-step')).toHaveText('Player 1 of 2');
  await expect(rows.filter({ hasText: 'Timber Wolf' })).toHaveCount(0);
  await expect(dlg.locator('#deploy-pilots-list .deploy-pilot', { hasText: 'Alrek' })).not.toContainText('in ');

  // Closing the wizard takes the plan out of the URL.
  await dlg.locator('#deploy-cancel').click();
  await expect(page).not.toHaveURL(/#deploy=/);
  await page.reload();
  await expect(dlg).toBeHidden();
});
