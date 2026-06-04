/* ============================================================
   agent-center docs — shared progressive enhancement.
   Pure vanilla, no deps. Everything degrades gracefully:
   without JS the page is fully readable; with reduced-motion
   all entrance animation is skipped.
   ============================================================ */
(function () {
  'use strict';
  var root = document.documentElement;
  root.classList.add('js'); /* gate reveal-hiding on JS being present */

  var reduce = window.matchMedia &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  /* ---------- 1. scroll reveal (staggered, ease-out) ---------- */
  var REVEAL = ['main .grid > .card', '.features > .feature',
                '.arch-server', '.arch-worker', '.prose'];
  var items = [];
  REVEAL.forEach(function (sel) {
    Array.prototype.forEach.call(document.querySelectorAll(sel), function (el) {
      if (el.closest('.tabpane')) return;      /* skip content hidden in tabs */
      if (items.indexOf(el) !== -1) return;
      el.setAttribute('data-reveal', '');
      items.push(el);
    });
  });
  /* stagger siblings within the same container */
  items.forEach(function (el) {
    var sibs = Array.prototype.filter.call(el.parentNode.children, function (c) {
      return c.hasAttribute && c.hasAttribute('data-reveal');
    });
    var i = sibs.indexOf(el);
    if (i > 0) el.style.transitionDelay = Math.min(i, 6) * 60 + 'ms';
  });

  if (reduce || !('IntersectionObserver' in window)) {
    items.forEach(function (el) { el.classList.add('in'); });
  } else {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) { e.target.classList.add('in'); io.unobserve(e.target); }
      });
    }, { rootMargin: '0px 0px -8% 0px', threshold: 0.08 });
    items.forEach(function (el) { io.observe(el); });
  }

  /* ---------- 2. copy buttons on code blocks ---------- */
  if (navigator.clipboard) {
    Array.prototype.forEach.call(document.querySelectorAll('.prose pre'), function (pre) {
      var btn = document.createElement('button');
      btn.className = 'copy-btn';
      btn.type = 'button';
      btn.textContent = 'copy';
      btn.setAttribute('aria-label', '复制代码');
      btn.addEventListener('click', function () {
        var code = pre.querySelector('code');
        var text = code ? code.innerText : pre.innerText;
        navigator.clipboard.writeText(text).then(function () {
          btn.textContent = 'copied'; btn.classList.add('ok');
          setTimeout(function () { btn.textContent = 'copy'; btn.classList.remove('ok'); }, 1400);
        });
      });
      pre.appendChild(btn);
    });
  }

  /* ---------- 3. TOC scrollspy (manual) ---------- */
  var toc = document.querySelector('.toc');
  if (toc) {
    var links = Array.prototype.slice.call(toc.querySelectorAll('a[href^="#"]'));
    var map = links.map(function (a) {
      return { a: a, el: document.getElementById(a.getAttribute('href').slice(1)) };
    }).filter(function (x) { return x.el; });

    if (map.length) {
      var current = null;
      var update = function () {
        var line = 100;                 /* px below viewport top = the read line */
        var active = map[0];
        for (var i = 0; i < map.length; i++) {
          if (map[i].el.getBoundingClientRect().top <= line) active = map[i];
          else break;
        }
        if (active.a !== current) {
          if (current) current.classList.remove('active');
          active.a.classList.add('active');
          current = active.a;
        }
      };
      var ticking = false;
      var onScroll = function () {
        if (ticking) return;
        ticking = true;
        window.requestAnimationFrame(function () { update(); ticking = false; });
      };
      window.addEventListener('scroll', onScroll, { passive: true });
      window.addEventListener('resize', onScroll, { passive: true });
      update();
    }
  }
})();
