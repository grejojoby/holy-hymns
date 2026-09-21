// Original Holy Hymns artwork. Uses Jimp already supplied by Expo's build tools.
const fs = require("node:fs");
const path = require("node:path");
const Jimp = require("jimp-compact");
const size = 1024;
const scale = 2;
const paper = "#F8F5ED";
const ink = "#214733";
function curve(start, control, end, steps = 32) {
  return Array.from({ length: steps + 1 }, (_, i) => {
    const t = i / steps;
    const u = 1 - t;
    return [
      u * u * start[0] + 2 * u * t * control[0] + t * t * end[0],
      u * u * start[1] + 2 * u * t * control[1] + t * t * end[1],
    ];
  });
}
const left = [
  ...curve([220, 474], [366, 414], [496, 525]),
  [496, 783],
  ...curve([496, 783], [365, 691], [220, 748]),
];
const leftInner = [
  ...curve([258, 502], [368, 465], [458, 545]),
  [458, 710],
  ...curve([458, 710], [366, 664], [258, 696]),
];
const mirror = (points) => points.map(([x, y]) => [1024 - x, y]);
const rectangle = (x, y, w, h) => [
  [x, y],
  [x + w, y],
  [x + w, y + h],
  [x, y + h],
];
const shapes = [
  [ink, rectangle(489, 225, 46, 270)],
  [ink, rectangle(399, 314, 226, 46)],
  [ink, left],
  [ink, mirror(left)],
  [paper, leftInner],
  [paper, mirror(leftInner)],
];
async function render(name, transparent) {
  const canvas = new Jimp(
    size * scale,
    size * scale,
    transparent ? 0x00000000 : 0xf8f5edff,
  );
  for (const [color, polygon] of shapes) {
    if (transparent && color === paper) continue;
    const points = polygon.map(([x, y]) => [x * scale, y * scale]);
    const rgba = Number.parseInt(color.slice(1) + "ff", 16);
    const minY = Math.floor(Math.min(...points.map((p) => p[1])));
    const maxY = Math.ceil(Math.max(...points.map((p) => p[1])));
    for (let y = minY; y <= maxY; y++) {
      const hits = [];
      for (let i = 0; i < points.length; i++) {
        const a = points[i],
          b = points[(i + 1) % points.length];
        if ((a[1] <= y && b[1] > y) || (b[1] <= y && a[1] > y))
          hits.push(a[0] + ((y - a[1]) * (b[0] - a[0])) / (b[1] - a[1]));
      }
      hits.sort((a, b) => a - b);
      for (let i = 0; i < hits.length; i += 2)
        for (let x = Math.ceil(hits[i]); x < hits[i + 1]; x++)
          canvas.setPixelColor(rgba, x, y);
    }
  }
  canvas.resize(size, size, Jimp.RESIZE_BICUBIC);
  await canvas.writeAsync(path.join(__dirname, name));
}
const polygons = shapes
  .map(
    ([fill, points]) =>
      `<polygon fill="${fill}" points="${points.map((p) => p.map((v) => v.toFixed(2)).join(",")).join(" ")}"/>`,
  )
  .join("\n");
fs.writeFileSync(
  path.join(__dirname, "icon.svg"),
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024"><title>Holy Hymns</title><desc>A cross above an open hymnbook, in forest green and warm paper.</desc><rect width="1024" height="1024" fill="${paper}"/>\n${polygons}\n</svg>\n`,
);
render("icon.png", false).then(() => {
  fs.copyFileSync(
    path.join(__dirname, "icon.png"),
    path.join(__dirname, "splash.png"),
  );
});
