---
id: scenario_004
default_next: ""
branches: [scenario_007, scenario_008]   # the December fronts: Twycross or Wotan
pilot_sp: 100   # SP each pilot who took part draws from the pool; the only place the amount is set
inputs:
  - id: data_core
    label: "Primary: a mech carrying the data core escaped off the eastern edge"
    scope: mission
    type: bool
    help: 600 SP
  - id: mist_lynx
    label: "Alternative: the repaired Mist Lynx was escorted off the eastern edge"
    scope: mission
    type: bool
    help: 1,000 SP, instead of the primary
  - id: falcon_mechs
    label: Jade Falcon mechs crippled or destroyed
    scope: mission
    max: 4
    help: 100 SP each
outcomes:
  # Getting the Mist Lynx out is the better result and replaces the primary's reward; either one is a success.
  - when: mist_lynx
    label: "The Mist Lynx was recovered"
    pool: 1000
    grants: [counter_intelligence]
  - when: data_core and not mist_lynx
    label: "The data core was recovered"
    pool: 600
  - pool: falcon_mechs * 100
aftermath:
  - when: data_core and not mist_lynx
    text: |
      The data from the C5 was safely returned to Cluster Command, though partially corrupted in transit.

      Command will make do with what could be deciphered. It will give our 341st Assault Cluster a material advantage in defending against the Falcons' 305th.
  - when: mist_lynx
    text: |
      The C5 was safely returned to Cluster Command, and its data was interpreted in full. The mission was a success.

      Armed with the enemy's intended attack corridors, the 341st sprang an ambush that was instrumental in decimating the Falcons' 305th. Your honorable victory in the field is noted. The Keshik is pleased.
---

# Scenario 004: Temper Edge - Outcome

## Victory & Rewards

A copy of the data core must get off the eastern edge, either in a mech that copied it or in the repaired Mist Lynx; otherwise the mission is a failure and is played again. The mission is also failed if the mech carrying the data core is destroyed while the Mist Lynx is destroyed or still unrepaired.

Objectives met fill the mission's pool of SP. Each pilot who took part draws a flat share from it (half for a pilot who sat the mission out, and nothing for a pilot killed in action, who is lost); whatever is left goes to the warchest.

| Objective                                                                     | SP    |
|-------------------------------------------------------------------------------|-------|
| Primary: escape off the eastern edge with a mech carrying the data core       | 600   |
| Alternative: escort the repaired Mist Lynx off the eastern edge (instead)     | 1,000 |
| Each Jade Falcon mech crippled or destroyed                                   | 100   |

## Keywords

- **Counter Intelligence** if the Mist Lynx was repaired and extracted.
