import { readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Preserve the original screenshot crops. The HD textures combine a newly
// generated illustration with native SVG typography, not enlarged screenshot text.
const root = dirname(fileURLToPath(import.meta.url));
const art = await readFile(resolve(root, 'assets/zoro-pack-art-hd.png'));
const image = `data:image/png;base64,${art.toString('base64')}`;
const shape = 'M0 0H1024V86L992 168V1562L1024 1650V1750H0V1650L32 1562V168L0 86Z';
const common = `<defs>
<clipPath id="bag"><path d="${shape}"/></clipPath>
<linearGradient id="foil" x1="0" x2="1"><stop stop-color="#581080"/><stop offset=".18" stop-color="#9b2ec0"/><stop offset=".43" stop-color="#69128d"/><stop offset=".76" stop-color="#b84acb"/><stop offset="1" stop-color="#3f075d"/></linearGradient>
<linearGradient id="seam" x1="0" x2="0" y1="0" y2="1"><stop stop-color="#b548d2"/><stop offset=".3" stop-color="#591080"/><stop offset=".62" stop-color="#7b169c"/><stop offset="1" stop-color="#c35ed9"/></linearGradient>
<linearGradient id="gold" x1="0" x2="0" y1="0" y2="1"><stop stop-color="#fff8c3"/><stop offset=".5" stop-color="#e4cb5c"/><stop offset="1" stop-color="#b98f32"/></linearGradient>
<linearGradient id="title" x1="0" x2="0" y1="0" y2="1"><stop stop-color="#fffefb"/><stop offset=".42" stop-color="#fff0ff"/><stop offset=".65" stop-color="#f6a9dc"/><stop offset="1" stop-color="#eec9ff"/></linearGradient>
<pattern id="crimp" width="9" height="12" patternUnits="userSpaceOnUse"><path d="M1 0V12" stroke="#f1a6ff" stroke-opacity=".32"/><path d="M5 0V12" stroke="#2a0046" stroke-opacity=".4"/></pattern>
</defs>`;
const seals = `<path d="M0 0H1024V67H0Z" fill="url(#seam)"/><path d="M0 0H1024V30H0Z" fill="url(#crimp)"/>
<path d="M0 1642H1024V1750H0Z" fill="url(#seam)"/><path d="M0 1709H1024V1750H0Z" fill="url(#crimp)"/>
<path d="M0 6H1024M0 1742H1024" stroke="#c369e8" stroke-width="4"/>`;
const header = `<path d="M20 188H1004V205H20Z" fill="#fffcec"/><path d="M22 207H1002V229H22Z" fill="url(#gold)"/><path d="M26 232H998V242H26Z" fill="#fffaf0"/>
<rect x="54" y="87" width="288" height="66" rx="5" fill="#fffef7"/><text x="198" y="135" text-anchor="middle" font-family="Arial Black,Arial,sans-serif" font-weight="900" font-size="44" fill="#172015" textLength="252" lengthAdjust="spacingAndGlyphs">ONE PIECE</text>
<rect x="735" y="57" width="236" height="44" rx="8" fill="#fff9df" stroke="#e4a53c" stroke-width="3"/><text x="853" y="87" text-anchor="middle" font-family="Arial,sans-serif" font-weight="bold" font-size="24" fill="#5a255f">BANDAI NAMCO</text>
<text x="963" y="152" text-anchor="end" fill="#fff8ff" font-family="Hiragino Kaku Gothic ProN,Noto Sans CJK JP,sans-serif" font-size="32" font-weight="700">ブースターパック</text>`;
const front = `<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1750" viewBox="0 0 1024 1750">${common}<g clip-path="url(#bag)">
<path d="${shape}" fill="url(#foil)"/>
<image width="1024" height="1750" href="${image}" preserveAspectRatio="none"/>
${seals}${header}
<g text-anchor="middle" font-family="Hiragino Mincho ProN,Yu Mincho,Noto Serif CJK JP,serif">
<text x="512" y="1190" font-size="26" letter-spacing="9" fill="#fff0ff">さいあくのせだい</text>
<text x="512" y="1338" font-size="148" font-weight="600" textLength="864" lengthAdjust="spacingAndGlyphs" fill="url(#title)" stroke="#552365" stroke-width="5" paint-order="stroke">最悪の世代</text>
</g>
<text x="512" y="1469" text-anchor="middle" font-family="Georgia,Times New Roman,serif" font-size="116" font-weight="bold" textLength="842" lengthAdjust="spacingAndGlyphs" fill="#fffdf1">ONE PIECE</text>
<text x="512" y="1522" text-anchor="middle" font-family="Arial,sans-serif" font-size="43" letter-spacing="12" fill="#fff7f7">CARD GAME</text>
<g font-family="Arial,sans-serif" fill="#f5e3fa" font-size="18"><text x="72" y="1575">ONE PIECE · RORONOA ZORO</text><text x="72" y="1604">THREE SWORD STYLE · COLLECTION</text><text x="72" y="1632" font-size="15" letter-spacing="3">DEMO EDITION</text></g>
<rect x="846" y="1554" width="119" height="110" rx="5" fill="#df2442" stroke="#fff3fa" stroke-width="4"/>
<g fill="#fff" font-family="Arial,sans-serif" font-size="40" font-weight="bold" text-anchor="middle"><text x="905" y="1601">BAN</text><text x="905" y="1643">DAI</text></g>
</g></svg>`;
const barcode = Array.from({ length: 46 }, (_, i) => `<rect x="${98 + i * 5}" y="1365" width="${i % 3 === 0 ? 3 : 1.8}" height="${i % 7 === 0 ? 123 : 110}"/>`).join('');
const back = `<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1750" viewBox="0 0 1024 1750">${common}<g clip-path="url(#bag)"><path d="${shape}" fill="url(#foil)"/>
<path d="M30 310 992 880 30 1440M994 310 30 880 994 1440" fill="none" stroke="#a257cc" stroke-width="8" opacity=".25"/>
${seals}${header}
<text x="512" y="422" text-anchor="middle" font-family="Georgia,serif" font-size="82" fill="#fff7f2">ONE PIECE</text><text x="512" y="475" text-anchor="middle" font-family="Arial,sans-serif" font-size="32" letter-spacing="12" fill="#ead5f4">CARD GAME</text>
<path d="M130 575H894V1198H130Z" fill="#3a0a59" stroke="#bc6ad2" stroke-width="3"/>
<g fill="#f2d9fa" text-anchor="middle" font-family="Hiragino Mincho ProN,Yu Mincho,serif"><text x="512" y="700" font-size="68">最悪の世代</text><text x="512" y="782" font-size="32" letter-spacing="8">ロロノア・ゾロ</text></g>
<g fill="none" stroke="#d2afe4" stroke-width="9"><path d="m323 1002 378-177m-378 0 378 177M512 820v252"/><path d="m327 965 28 51m314-51 28 51m-211-1h54" stroke="#e4c484" stroke-width="16"/></g>
<text x="512" y="1126" text-anchor="middle" fill="#dab9ee" font-family="Arial,sans-serif" font-size="23" letter-spacing="5">THREE SWORD STYLE</text>
<rect x="72" y="1264" width="880" height="335" rx="4" fill="#fffdf5"/><rect x="72" y="1264" width="880" height="63" fill="#171020"/>
<text x="101" y="1306" fill="#fff" font-family="Arial,sans-serif" font-size="27">ZORO COLLECTION · DEMO</text>
<g fill="#24172e">${barcode}</g>
<g fill="#3c294a" font-family="Arial,sans-serif" font-size="25"><text x="391" y="1388">ONE PIECE CARD GAME</text><text x="391" y="1436">RORONOA ZORO</text><text x="391" y="1484">COLLECTIBLE PACK · 10</text><text x="391" y="1542" font-size="20">LOCAL ANIMATION DEMONSTRATION</text></g>
</g></svg>`;
await Promise.all([
 writeFile(resolve(root, 'assets/zoro-pack-front-hd.svg'), front),
 writeFile(resolve(root, 'assets/zoro-pack-back-hd.svg'), back),
]);
console.log('Built HD pack textures: 1024×1750, vector typography and shared illustration.');
