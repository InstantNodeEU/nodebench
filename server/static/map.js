// Zoom and pan for the location maps. The map is plain SVG, so zooming is
// just a smaller viewBox; dots and labels are scaled back down so they keep
// their size on screen while the coastline gets bigger.
(function () {
	"use strict";

	var MAX = 8;

	function setup(svg) {
		var vb = svg.viewBox.baseVal;
		var W = vb.width, H = vb.height;
		var view = { x: 0, y: 0, w: W, h: H };

		var dots = [].slice.call(svg.querySelectorAll("circle"));
		dots.forEach(function (c) { c.dataset.r = c.getAttribute("r"); });
		var labels = [].slice.call(svg.querySelectorAll("text[data-sx]"));
		var callout = svg.querySelector(".callout");
		var fine = svg.querySelector("image.fine");

		var box = document.createElement("div");
		box.className = "map-ctl";
		box.innerHTML =
			'<button type="button" data-z="in" aria-label="Zoom in">+</button>' +
			'<button type="button" data-z="out" aria-label="Zoom out">&minus;</button>' +
			'<button type="button" data-z="eu">Europe</button>' +
			'<button type="button" data-z="all">World</button>';
		svg.parentNode.classList.add("map-host");
		svg.parentNode.insertBefore(box, svg.nextSibling);

		var hint = document.createElement("span");
		hint.className = "map-hint";
		hint.textContent = "Drag to move, Ctrl + scroll or pinch to zoom";
		box.appendChild(hint);

		function apply() {
			view.w = Math.min(W, Math.max(W / MAX, view.w));
			view.h = view.w * H / W;
			view.x = Math.min(W - view.w, Math.max(0, view.x));
			view.y = Math.min(H - view.h, Math.max(0, view.y));
			svg.setAttribute("viewBox", view.x + " " + view.y + " " + view.w + " " + view.h);

			var k = W / view.w;
			// dots grow a little when zoomed so the change is visible, but much
			// slower than the map itself
			var dk = Math.pow(k, 0.75);
			dots.forEach(function (c) { c.setAttribute("r", c.dataset.r / dk); });
			labels.forEach(function (t) {
				t.setAttribute("x", +t.dataset.sx + t.dataset.dx / k);
				t.setAttribute("y", +t.dataset.sy + 4 / k);
				t.style.fontSize = (12 / k) + "px";
			});
			if (k > 1.5 && fine && !fine.getAttribute("href")) fine.setAttribute("href", fine.dataset.href);
			svg.classList.toggle("fine-on", k > 1.5);
			svg.classList.toggle("zoomed", k > 2.2);
			// on touch screens the page keeps scrolling until the map is zoomed in
			svg.classList.toggle("in", k > 1.01);
			if (callout) callout.style.display = k > 2.2 ? "none" : "";
		}

		// zoom by f around a point given in viewBox units
		function zoom(f, cx, cy) {
			if (cx === undefined) { cx = view.x + view.w / 2; cy = view.y + view.h / 2; }
			var nw = Math.min(W, Math.max(W / MAX, view.w / f));
			var s = nw / view.w;
			view.x = cx - (cx - view.x) * s;
			view.y = cy - (cy - view.y) * s;
			view.w = nw;
			apply();
		}

		function toMap(clientX, clientY) {
			var r = svg.getBoundingClientRect();
			return { x: view.x + (clientX - r.left) / r.width * view.w, y: view.y + (clientY - r.top) / r.height * view.h };
		}

		box.addEventListener("click", function (e) {
			var z = e.target.dataset && e.target.dataset.z;
			if (z === "in") zoom(1.6);
			else if (z === "out") zoom(1 / 1.6);
			else if (z === "all") { view = { x: 0, y: 0, w: W, h: H }; apply(); }
			else if (z === "eu") {
				// lon -12..32, lat 61..34 in the map's projection
				view.w = W * 44 / 360;
				view.x = (168 / 360) * W;
				view.y = (80 - 62) * W / 360;
				apply();
			}
		});

		svg.addEventListener("wheel", function (e) {
			if (!e.ctrlKey && !e.metaKey) return;
			e.preventDefault();
			var p = toMap(e.clientX, e.clientY);
			zoom(Math.exp(-e.deltaY * 0.004), p.x, p.y);
		}, { passive: false });

		svg.addEventListener("dblclick", function (e) {
			var p = toMap(e.clientX, e.clientY);
			zoom(2, p.x, p.y);
		});

		// pan with one pointer, pinch with two
		var pts = {}, last = null, pinch = 0;
		svg.addEventListener("pointerdown", function (e) {
			pts[e.pointerId] = { x: e.clientX, y: e.clientY };
			svg.setPointerCapture(e.pointerId);
			last = { x: e.clientX, y: e.clientY };
			var ids = Object.keys(pts);
			if (ids.length === 2) pinch = dist(pts[ids[0]], pts[ids[1]]);
		});
		svg.addEventListener("pointermove", function (e) {
			if (!pts[e.pointerId]) return;
			pts[e.pointerId] = { x: e.clientX, y: e.clientY };
			var ids = Object.keys(pts);
			if (ids.length === 2) {
				var d = dist(pts[ids[0]], pts[ids[1]]);
				if (pinch > 0) {
					var m = toMap((pts[ids[0]].x + pts[ids[1]].x) / 2, (pts[ids[0]].y + pts[ids[1]].y) / 2);
					zoom(d / pinch, m.x, m.y);
				}
				pinch = d;
				return;
			}
			if (view.w >= W) return;
			var r = svg.getBoundingClientRect();
			view.x -= (e.clientX - last.x) / r.width * view.w;
			view.y -= (e.clientY - last.y) / r.height * view.h;
			last = { x: e.clientX, y: e.clientY };
			apply();
		});
		function up(e) {
			delete pts[e.pointerId];
			pinch = 0;
			var ids = Object.keys(pts);
			if (ids.length === 1) last = pts[ids[0]];
		}
		svg.addEventListener("pointerup", up);
		svg.addEventListener("pointercancel", up);

		apply();
	}

	function dist(a, b) { return Math.hypot(a.x - b.x, a.y - b.y); }

	document.querySelectorAll("svg.map").forEach(setup);
})();
