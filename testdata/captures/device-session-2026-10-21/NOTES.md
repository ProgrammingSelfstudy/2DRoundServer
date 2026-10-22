# Device session 2026-10-21 (Samsung SM-A165F, role 玩家9, after the batch 2 capture changes)

Built and restarted `devall` with: tavern shop 13403 type 4 (the captured six goods), 16711 order
(16710 first, empty msg), 23234 answered with 23235 then 23234, free single draw in group 300 and the
05:00 reset. `go test -race ./...` and `cmd/bot` passed before the phone was used.

- **Tavern shop (市场)**: shows the six goods with their discounts and prices, same as the original
  on the vivo (`tavern-shop-list.png`). Buying and refreshing are not answered (replies not captured).
- **Advanced summon**: the single draw button says 免费召唤 (group 300 free draw); one free draw gave a
  5-star hero with the show animation, then the result page with the hero card
  (`summon-advanced-free-single.png`).
- **Basic summon ten-draw**: the result page lists ten hero cards of 1 to 3 stars
  (`summon-ten-low-star-result.png`). The low-star heroes that used to show nothing now show.
- Not checked on the phone: VIP page, seer palace open (23234), ticket count after the draws.
