// A pool of BattleTech-sounding first names (Clan warriors and Inner Sphere MechWarriors), used as the name boxes' placeholder. The portraits are gendered,
// so the placeholder follows the portrait's group: Male and Female portraits draw from their own names, anything else (Generic, Special) from all of them.
// A campaign can replace any of these lists with its own (its Names.md, sent as this script tag's data-names), so the suggestions fit its faction and era.
var PilotNames = (function () {
  var own = {};
  try { own = JSON.parse((document.currentScript && document.currentScript.getAttribute('data-names')) || '{}') || {}; } catch (e) { own = {}; }
  function list(key, baseline) { return own[key] && own[key].length ? own[key] : baseline; }
  var MALE = ['Alrek', 'Vandal', 'Aidan', 'Brendt', 'Ravill', 'Phelan', 'Elias', 'Ulric', 'Kael', 'Tormund', 'Conal', 'Severin', 'Jorge', 'Radick', 'Evander', 'Casimir', 'Lucan', 'Garrick', 'Roderick', 'Dmitri', 'Ardan', 'Sven', 'Corwin', 'Lysander', 'Hector', 'Maddox'];
  var FEMALE = ['Katarina', 'Marthe', 'Joanna', 'Diana', 'Natasha', 'Dagmar', 'Ilsa', 'Yvonne', 'Mira', 'Brianna', 'Thea', 'Anastasia', 'Selene', 'Vesna', 'Tyra', 'Malvina', 'Ysolde', 'Petra', 'Renna'];
  var NEUTRAL = ['Sorrel', 'Oriel', 'Hollis', 'Kerensky', 'Bishop'];
  MALE = list('male', MALE); FEMALE = list('female', FEMALE); NEUTRAL = list('neutral', NEUTRAL);
  function pick(list) { return list[Math.floor(Math.random() * list.length)]; }
  // group is a portrait's group ("Male", "Female", "Generic", "Special"); anything else draws from every name.
  function random(group) {
    var pool = group === 'Male' ? MALE.concat(NEUTRAL) : group === 'Female' ? FEMALE.concat(NEUTRAL) : MALE.concat(FEMALE, NEUTRAL);
    return pick(pool);
  }
  // The group of the portrait with this file name, from the page's portrait list.
  function groupOf(list, file) {
    for (var i = 0; i < list.length; i++) if (list[i].file === file) return list[i].group;
    return '';
  }
  // A pool of BattleTech-sounding force names for the force-name box's placeholder.
  var FORCES = ["Wolf's Dragoons", 'Kell Hounds', 'Gray Death Legion', 'Northwind Highlanders', 'Eridani Light Horse', "Snord's Irregulars", 'Black Widow Company', 'Lone Star Lancers', 'Outworlds Rangers', 'Crimson Talons', 'Iron Hand Company', 'Sable Wardens', 'Pyre Guard', 'Stormcrow Cadre'];
  FORCES = list('forces', FORCES);
  function randomForce() { return pick(FORCES); }
  return { random: random, groupOf: groupOf, randomForce: randomForce };
})();
