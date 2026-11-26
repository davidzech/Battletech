---
id: scenario_002
default_next: ""
branches: [scenario_004, scenario_005]   # Baker 3, or MacDuff (only from Sudeten)
pilot_sp: 100   # SP each pilot who took part draws from the pool; the only place the amount is set
objectives:
  - id: buhallin_killed
    label: "Primary: assassinate Star Colonel Devin Buhallin in the hardened HQ"
    sp: 400
    required: true
  - id: fuel_station
    label: "Secondary: destroy the DropShip fuel pumping station"
    sp: 300
    required: true
  - id: mattlov_killed
    label: "Optional: kill Star Captain Rothgar Mattlov's Summoner"
    sp: 200
    grants: [no_quarter]
    unmet_grants: [avenger]
  - id: target_of_opportunity
    label: "Optional: find and destroy the target of opportunity"
    sp: 100
    unmet_grants: [sigint]
inputs:
  - id: orbital_scan
    label: Orbital scan bought for reconnaissance
    scope: mission
    type: bool
    help: costs 100 SP
outcomes:
  - when: orbital_scan
    warchest: -100
---

# Scenario 002: Flame Tongue - Outcome

## Victory & Rewards

Both the Primary and the Secondary objective must be met, or the mission is a failure and is played again.

Objectives met fill the mission's pool of SP. Each pilot who took part draws a flat share from it (half for a pilot who sat the mission out, and nothing for a pilot killed in action, who is lost); whatever is left goes to the warchest. An orbital scan bought for reconnaissance costs the warchest 100 SP.

| Objective                                                    | SP         |
|--------------------------------------------------------------|------------|
| Primary: assassinate Star Colonel Devin Buhallin             | 400        |
| Secondary: destroy the DropShip fuel pumping station         | 300        |
| Optional: kill Star Captain Rothgar Mattlov's Summoner       | 200        |
| Optional: find and destroy the target of opportunity         | 100        |

## Keywords

- **SIGINT** if the target of opportunity was not destroyed.
- **Avenger** if Star Captain Rothgar Mattlov was not killed; **No Quarter** if he was.

## Aftermath

After engaging the Falcon garrison at Webster, Khan Phelan ordered his attack force to retreat to the Dargoth Plains. Under the cover of Wolf aerospace fighters, Khan Phelan's 'Mechs fired on the pursuing Falcon defenders as they went, and destroyed many of the Falcons' heavy and assault 'Mechs before they reached the plains.

When the surviving Falcon units crested the first hills of Dargoth's grassy plains, they saw what appeared to be a disorganized army desperately trying to reach its DropShips some fifteen miles away. Then the hidden Wolf 'Mechs emerged from the tree-covered hills, and the Falcons found themselves surrounded. Waves of Wolf aerospace fighters dropped their remaining bombs on the trapped Falcons, and the battered Falcon 'Mechs made easy targets for the fresh Wolf BattleMechs.

In the end, Khan Phelan's forces scored an impressive victory. They killed or captured some 100 Falcon 'Mechs, nearly all that had been on the planet. Eighty-five percent of the Falcon MechWarriors and pilots were killed, and the rest were injured. The Wolves lost only 35 units.

After returning to the Wolf fleet, Khan Phelan tried to persuade Khan Natasha to include his forces in further battles, using his victory to bolster his arguments. Despite his protests, Khan Natasha remained adamant that he follow Ulric's original plan and lead Delta Galaxy into the Inner Sphere. On October 5, 3057, Khan Phelan's JumpShip *Werewolf* left Clan space with a very special cargo: the complete genetic heritage and future of the Wolf Clan.
