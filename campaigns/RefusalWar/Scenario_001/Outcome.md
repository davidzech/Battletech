---
id: scenario_001
default_next: ""
branches: [scenario_002, scenario_003]   # a choice of next mission: Flame Tongue (Sudeten) or Blade Splint (Zoetermeer)
pilot_sp: 100   # SP each pilot who took part draws from the pool; the only place the amount is set
objectives:
  - id: plant_large
    label: "Primary: destroy the chemical plant, large building"
    sp: 400
    required: true
  - id: plant_small
    label: "Primary: destroy the chemical plant, small building"
    sp: 250
    required: true
  - id: firebase_inspected
    label: "Secondary: inspect the firebase"
    sp: 450
  - id: objective_1
    label: "Objective 1: occupy a hex adjacent to the supply transport"
    sp: 100
  - id: objective_5
    label: "Objective 5: destroy the firebase remains"
    sp: 100
inputs:
  - id: falcon_mechs
    label: Jade Falcon mechs crippled or destroyed
    scope: mission
    max: 5
    help: 200 SP each
outcomes:
  - pool: falcon_mechs * 200
aftermath:
  - when: plant_large and plant_small
    text: |
      Your strike has left the chemical plant in ruins. No doubt the Falcon Regulars will be unable to fully resupply.

      The Keshik is pleased.
---

# Scenario 001: Pyre Light - Outcome

## Victory & Rewards

Objectives met fill the mission's pool of SP. Each pilot who took part draws a flat share from it (half for a pilot who sat the mission out, and nothing for a pilot killed in action, who is lost); whatever is left goes to the warchest.

| Objective                                                    | SP         |
|--------------------------------------------------------------|------------|
| Primary: destroy the chemical plant, large building          | 400        |
| Primary: destroy the chemical plant, small building          | 250        |
| Secondary: inspect the firebase                              | 450        |
| Each Jade Falcon mech crippled or destroyed                  | 200        |
| Objective 1: occupy an adjacent hex                           | 100        |
| Objective 5: destroy the firebase remains                     | 100        |

## Aftermath

The bitter rivalry between the Wolves and the Jade Falcons fueled fierce fighting on Colmar, a clash that set the tone for the entire Refusal War.

## Next Steps

Colmar is free, and the Falcons are reeling. The Keshik gives you the choice of where to strike next.

- **Flame Tongue**, on Sudeten: saKhan Phelan's Delta Galaxy will not leave for the Inner Sphere before it draws Falcon blood. Hunt down Star Colonel Devin Buhallin in the 8th Falcon Regulars' headquarters, and leave his command nothing to fly away on.
- **Blade Splint**, on Zoetermeer: the 10th Provisional Garrison has hidden its headquarters in the city of Salyn. Find the city's plasma converter and learn what it burns.
