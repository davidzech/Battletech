---
id: scenario_001
default_next: ""
pilot_sp: 100
objectives:
  - id: plant_large
    label: "Primary: destroy the chemical plant, large building"
    sp: 400
  - id: plant_small
    label: "Primary: destroy the chemical plant, small building"
    sp: 250
  - id: firebase_inspected
    label: "Secondary: inspect the firebase"
    sp: 450
  - id: waypoint_a
    label: "Waypoint A: occupy a hex adjacent to the supply transport"
    sp: 100
  - id: waypoint_e
    label: "Waypoint E: destroy the firebase remains"
    sp: 100
inputs:
  - id: falcon_mechs
    label: Jade Falcon mechs crippled or destroyed
    scope: mission
    max: 5
    help: 200 SP each
outcomes:
  - pool: falcon_mechs * 200
    label: "Jade Falcon mechs crippled or destroyed"
---

# Scenario 001: Pyre Light - Outcome

## Victory & Rewards

Objectives met fill the mission's pool of SP. Each pilot who took part draws 100 SP from it (a pilot who sat the mission out draws 50, and a pilot killed in action draws nothing and is lost); whatever is left goes to the warchest.

| Objective                                                    | SP         |
|--------------------------------------------------------------|------------|
| Primary: destroy the chemical plant, large building          | 400        |
| Primary: destroy the chemical plant, small building          | 250        |
| Secondary: inspect the firebase                              | 450        |
| Each Jade Falcon mech crippled or destroyed                  | 200        |
| Waypoint A: occupy an adjacent hex                           | 100        |
| Waypoint E: destroy the firebase remains                     | 100        |
