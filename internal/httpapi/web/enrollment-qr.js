'use strict';
(() => {
  const SVG = 'http://www.w3.org/2000/svg';
  function render(container, url) {
    container.replaceChildren();
    try {
      const qr = window.qrcode(0, 'M');
      qr.addData(url, 'Byte');
      qr.make();
      const modules = qr.getModuleCount();
      const quiet = 4;
      const size = modules + quiet * 2;
      const image = document.createElementNS(SVG, 'svg');
      image.setAttribute('viewBox', `0 0 ${size} ${size}`);
      image.setAttribute('width', String(size * 4));
      image.setAttribute('height', String(size * 4));
      image.setAttribute('role', 'img');
      image.setAttribute('aria-label', 'QR-код подключения планшета');
      image.setAttribute('shape-rendering', 'crispEdges');
      const background = document.createElementNS(SVG, 'rect');
      background.setAttribute('width', String(size));
      background.setAttribute('height', String(size));
      background.setAttribute('fill', '#fff');
      const path = document.createElementNS(SVG, 'path');
      const squares = [];
      for (let row = 0; row < modules; row++) {
        for (let col = 0; col < modules; col++) {
          if (qr.isDark(row, col)) squares.push(`M${col + quiet} ${row + quiet}h1v1h-1z`);
        }
      }
      path.setAttribute('d', squares.join(''));
      path.setAttribute('fill', '#000');
      image.append(background, path);
      container.append(image);
      return true;
    } catch {
      // The copyable enrollment link remains usable if encoding fails.
      container.replaceChildren();
      return false;
    }
  }
  window.DonEnrollmentQR = { render };
})();
