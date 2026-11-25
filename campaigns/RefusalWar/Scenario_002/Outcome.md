---
id: scenario_002
default_next: ""
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
