document.addEventListener("DOMContentLoaded", function () {
    const map = new maplibregl.Map({
        container: "map",
        style: "https://tiles.openfreemap.org/styles/liberty",
        center: [10, 50],
        zoom: 4,
        pitch: 25,
    });

    map.addControl(new maplibregl.NavigationControl({
        visualizePitch: true,
    }));
    map.addControl(new maplibregl.TerrainControl({
        source: "terrain",
        exaggeration: 1.5,
    }));

    const markers = [];

    map.on("load", function () {
        // 3D terrain
        map.addSource("terrain", {
            type: "raster-dem",
            tiles: ["https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"],
            encoding: "terrarium",
            tileSize: 256,
            maxzoom: 15,
        });
        map.setTerrain({ source: "terrain", exaggeration: 1.5 });

        map.addSource("hillshade-source", {
            type: "raster-dem",
            tiles: ["https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"],
            encoding: "terrarium",
            tileSize: 256,
            maxzoom: 15,
        });
        map.addLayer({
            id: "hillshade",
            type: "hillshade",
            source: "hillshade-source",
            paint: {
                "hillshade-shadow-color": "#473B24",
                "hillshade-illumination-anchor": "map",
                "hillshade-exaggeration": 0.5,
            },
        }, "building");
        fetch("/t/" + shareToken + "/track")
            .then(function (r) {
                return r.json();
            })
            .then(function (geojson) {
                // Add track line source
                map.addSource("track", {
                    type: "geojson",
                    data: {
                        type: "FeatureCollection",
                        features: geojson.features.filter(function (f) {
                            return f.properties.type === "track";
                        }),
                    },
                });

                map.addLayer({
                    id: "track-line",
                    type: "line",
                    source: "track",
                    paint: {
                        "line-color": "#b85c38",
                        "line-width": 3.5,
                    },
                });

                // Add entry markers
                var entryFeatures = geojson.features.filter(function (f) {
                    return f.properties.type === "entry";
                });

                entryFeatures.forEach(function (f) {
                    var el = document.createElement("div");
                    if (f.properties.photo) {
                        el.className = "map-marker-photo";
                        var img = document.createElement("img");
                        img.src = "/uploads/" + f.properties.photo;
                        el.appendChild(img);
                    } else {
                        el.className = "map-marker";
                    }

                    var marker = new maplibregl.Marker({ element: el })
                        .setLngLat(f.geometry.coordinates)
                        .addTo(map);

                    // Click marker -> scroll to entry
                    el.addEventListener("click", function () {
                        if (typeof isMobile !== "undefined" && isMobile) {
                            // Mobile: scroll card strip to this entry
                            var card = document.querySelector('.mobile-entry-card[data-entry-id="' + f.properties.id + '"]');
                            if (card) {
                                card.scrollIntoView({ behavior: "smooth", inline: "center", block: "nearest" });
                            }
                        } else {
                            // Desktop: scroll timeline
                            var entryEl = document.querySelector('.timeline-entry[data-entry-id="' + f.properties.id + '"]');
                            if (entryEl) {
                                entryEl.scrollIntoView({ behavior: "smooth", block: "center" });
                                highlightEntry(entryEl);
                            }
                        }
                    });

                    markers.push({
                        marker: marker,
                        entryId: f.properties.id,
                    });
                });

                // Current position marker (last point of the track)
                var trackFeature = geojson.features.find(function (f) {
                    return f.properties.type === "track";
                });
                var currentPosMarker = null;
                if (trackFeature && trackFeature.geometry.coordinates.length > 0) {
                    var lastCoord = trackFeature.geometry.coordinates[trackFeature.geometry.coordinates.length - 1];
                    var el = document.createElement("div");
                    el.className = "map-marker-current";
                    el.innerHTML = '<svg width="36" height="48" viewBox="0 0 36 48" fill="none" xmlns="http://www.w3.org/2000/svg">' +
                        '<path d="M18 0C8.06 0 0 8.06 0 18c0 12.6 18 30 18 30s18-17.4 18-30C36 8.06 27.94 0 18 0z" fill="#b85c38"/>' +
                        '<circle cx="18" cy="18" r="8" fill="white"/>' +
                        '<circle cx="18" cy="18" r="4" fill="#b85c38"/>' +
                        '</svg>';
                    currentPosMarker = new maplibregl.Marker({ element: el, anchor: "bottom" })
                        .setLngLat(lastCoord)
                        .addTo(map);
                }

                // Fit bounds to track + markers
                fitMapBounds(geojson);

                // SSE live updates
                var evtSource = new EventSource("/t/" + shareToken + "/sse");

                evtSource.addEventListener("trackpoint", function (e) {
                    var point = JSON.parse(e.data);
                    var trackSource = map.getSource("track");
                    if (trackSource) {
                        var data = trackSource._data || trackSource.serialize().data;
                        if (data && data.features && data.features.length > 0) {
                            data.features[0].geometry.coordinates.push([point.lon, point.lat]);
                            trackSource.setData(data);
                        }
                    }
                    // Move current position marker
                    if (currentPosMarker) {
                        currentPosMarker.setLngLat([point.lon, point.lat]);
                    }
                });

                evtSource.addEventListener("entry", function () {
                    // Reload the page to show the new entry
                    window.location.reload();
                });
            });
    });

    // Scroll timeline -> pan map (center-based tracking)
    var timelineEl = document.getElementById("timeline");
    var allEntries = Array.from(document.querySelectorAll(".timeline-entry[data-lat]"));
    var activeEntryId = null;

    function updateActiveEntry() {
        var timelineRect = timelineEl.getBoundingClientRect();
        var centerY = timelineRect.top + timelineRect.height * 0.2;

        var closest = null;
        var closestDist = Infinity;

        allEntries.forEach(function (el) {
            var dot = el.querySelector(".timeline-dot");
            if (!dot) return;
            var dotRect = dot.getBoundingClientRect();
            var dotCenter = dotRect.top + dotRect.height / 2;
            var dist = Math.abs(dotCenter - centerY);
            if (dist < closestDist) {
                closestDist = dist;
                closest = el;
            }
        });

        if (!closest) return;
        var entryId = closest.dataset.entryId;
        if (entryId === activeEntryId) return;
        activeEntryId = entryId;

        var lat = parseFloat(closest.dataset.lat);
        var lon = parseFloat(closest.dataset.lon);
        if (!isNaN(lat) && !isNaN(lon)) {
            map.flyTo({
                center: [lon, lat],
                zoom: Math.max(map.getZoom(), 10),
                duration: 500,
            });
        }

        highlightEntry(closest);

        markers.forEach(function (m) {
            var el = m.marker.getElement();
            if (String(m.entryId) === String(entryId)) {
                el.classList.add("map-marker-active");
            } else {
                el.classList.remove("map-marker-active");
            }
        });
    }

    var scrollTimeout;
    timelineEl.addEventListener("scroll", function () {
        clearTimeout(scrollTimeout);
        scrollTimeout = setTimeout(updateActiveEntry, 80);
    });
    // Scroll desktop timeline to the bottom (latest entries)
    timelineEl.scrollTop = timelineEl.scrollHeight;
    updateActiveEntry();

    // Stop timeline line at the current entry's dot
    var currentEntry = document.querySelector(".timeline-current");
    if (currentEntry) {
        var line = currentEntry.closest(".timeline-line");
        if (line) {
            var dot = currentEntry.querySelector(".timeline-dot");
            var dotBottom = dot.getBoundingClientRect().bottom;
            var lineBottom = line.getBoundingClientRect().bottom;
            line.style.setProperty("--current-height", (lineBottom - dotBottom) + "px");
        }
    }

    // Click timeline dots or dates -> fly to location on map
    document.querySelectorAll(".timeline-dot, .timeline-date").forEach(function (el) {
        el.style.cursor = "pointer";
        el.addEventListener("click", function () {
            var entry = el.closest(".timeline-entry");
            if (!entry) return;
            var lat = parseFloat(entry.dataset.lat);
            var lon = parseFloat(entry.dataset.lon);
            if (!isNaN(lat) && !isNaN(lon)) {
                map.flyTo({
                    center: [lon, lat],
                    zoom: Math.max(map.getZoom(), 12),
                    duration: 800,
                });
                highlightEntry(entry);
                var entryId = entry.dataset.entryId;
                markers.forEach(function (m) {
                    var mel = m.marker.getElement();
                    if (String(m.entryId) === String(entryId)) {
                        mel.classList.add("map-marker-active");
                    } else {
                        mel.classList.remove("map-marker-active");
                    }
                });
            }
        });
    });

    // Position distance labels at the midpoint between consecutive dots
    positionDistanceLabels();

    function positionDistanceLabels() {
        var dots = document.querySelectorAll(".timeline-dot");
        var labels = document.querySelectorAll(".timeline-distance-label");
        var entries = document.querySelectorAll(".timeline-entry");

        var labelIdx = 0;
        for (var i = 0; i < entries.length; i++) {
            if (!entries[i].classList.contains("has-distance")) continue;
            var label = labels[labelIdx];
            if (!label || i === 0) { labelIdx++; continue; }

            // Find the previous dot and current dot positions (relative to timeline)
            var prevDot = entries[i - 1].querySelector(".timeline-dot");
            var curDot = entries[i].querySelector(".timeline-dot");
            var prevRect = prevDot.getBoundingClientRect();
            var curRect = curDot.getBoundingClientRect();

            // Midpoint between the two dots
            var midY = (prevRect.top + prevRect.bottom + curRect.top + curRect.bottom) / 4;

            // Position label relative to its parent entry
            var entryRect = entries[i].getBoundingClientRect();
            label.style.top = (midY - entryRect.top) + "px";

            labelIdx++;
        }
    }

    // Lightbox
    var lightbox = document.createElement("div");
    lightbox.id = "lightbox";
    lightbox.innerHTML =
        '<div class="lightbox-backdrop"></div>' +
        '<div class="lightbox-media"></div>' +
        '<button class="lightbox-prev">&lsaquo;</button>' +
        '<button class="lightbox-next">&rsaquo;</button>' +
        '<button class="lightbox-close">&times;</button>' +
        '<span class="lightbox-counter"></span>';
    document.body.appendChild(lightbox);

    var lightboxMedia = lightbox.querySelector(".lightbox-media");
    var lightboxCounter = lightbox.querySelector(".lightbox-counter");
    var currentMedia = [];
    var currentIndex = 0;

    function openLightbox(el) {
        var container = el.closest(".timeline-photos");
        if (!container) return;
        currentMedia = Array.from(container.querySelectorAll("img, video"));
        currentIndex = currentMedia.indexOf(el);
        if (currentIndex === -1) currentIndex = 0;
        showMedia();
        lightbox.classList.add("active");
    }

    function showMedia() {
        var el = currentMedia[currentIndex];
        var isVideo = el.tagName === "VIDEO";
        if (isVideo) {
            lightboxMedia.innerHTML = '<video class="lightbox-img" src="' + el.src + '" controls autoplay></video>';
        } else {
            var fullSrc = el.dataset.full || el.src;
            lightboxMedia.innerHTML = '<img class="lightbox-img" src="' + fullSrc + '">';
        }
        lightboxCounter.textContent = (currentIndex + 1) + " / " + currentMedia.length;
    }

    function closeLightbox() {
        lightbox.classList.remove("active");
        lightboxMedia.innerHTML = "";
    }

    lightbox.querySelector(".lightbox-backdrop").addEventListener("click", closeLightbox);
    lightbox.querySelector(".lightbox-close").addEventListener("click", closeLightbox);

    lightbox.querySelector(".lightbox-prev").addEventListener("click", function (e) {
        e.stopPropagation();
        currentIndex = (currentIndex - 1 + currentMedia.length) % currentMedia.length;
        showMedia();
    });

    lightbox.querySelector(".lightbox-next").addEventListener("click", function (e) {
        e.stopPropagation();
        currentIndex = (currentIndex + 1) % currentMedia.length;
        showMedia();
    });

    document.addEventListener("keydown", function (e) {
        if (!lightbox.classList.contains("active")) return;
        if (e.key === "Escape") closeLightbox();
        if (e.key === "ArrowLeft") {
            currentIndex = (currentIndex - 1 + currentMedia.length) % currentMedia.length;
            showMedia();
        }
        if (e.key === "ArrowRight") {
            currentIndex = (currentIndex + 1) % currentMedia.length;
            showMedia();
        }
    });

    document.querySelectorAll(".timeline-photos img").forEach(function (el) {
        el.style.cursor = "pointer";
        el.addEventListener("click", function () {
            openLightbox(el);
        });
    });

    document.querySelectorAll(".timeline-photos .video-wrap").forEach(function (wrap) {
        var video = wrap.querySelector("video");
        wrap.style.cursor = "pointer";
        wrap.addEventListener("click", function () {
            openLightbox(video);
        });
    });

    // Photo scroll arrows
    document.querySelectorAll(".timeline-photos-wrap").forEach(function (wrap) {
        var strip = wrap.querySelector(".timeline-photos");
        var left = wrap.querySelector(".scroll-arrow-left");
        var right = wrap.querySelector(".scroll-arrow-right");

        function updateArrows() {
            var canScrollLeft = strip.scrollLeft > 5;
            var canScrollRight = strip.scrollLeft < strip.scrollWidth - strip.clientWidth - 5;
            left.classList.toggle("visible", canScrollLeft);
            right.classList.toggle("visible", canScrollRight);
        }

        strip.addEventListener("scroll", updateArrows);
        updateArrows();

        left.addEventListener("click", function () {
            strip.scrollLeft -= strip.clientWidth * 0.7;
        });
        right.addEventListener("click", function () {
            strip.scrollLeft += strip.clientWidth * 0.7;
        });
    });

    function highlightEntry(el) {
        document
            .querySelectorAll(".timeline-entry.active")
            .forEach(function (e) {
                e.classList.remove("active");
            });
        el.classList.add("active");
    }

    function fitMapBounds(geojson) {
        var bounds = null;

        geojson.features.forEach(function (f) {
            if (f.geometry.type === "LineString") {
                f.geometry.coordinates.forEach(function (c) {
                    if (!bounds) {
                        bounds = new maplibregl.LngLatBounds(c, c);
                    } else {
                        bounds.extend(c);
                    }
                });
            } else if (f.geometry.type === "Point") {
                var c = f.geometry.coordinates;
                if (!bounds) {
                    bounds = new maplibregl.LngLatBounds(c, c);
                } else {
                    bounds.extend(c);
                }
            }
        });

        if (bounds) {
            map.fitBounds(bounds, { padding: 50, maxZoom: 14 });
        }
    }

    // ========================================
    // Mobile: horizontal timeline + entry viewer
    // ========================================
    if (typeof isMobile !== "undefined" && isMobile) {
        var mobileCards = document.querySelectorAll(".mobile-entry-card");
        var viewer = document.getElementById("mobile-viewer");
        var viewerProgress = viewer.querySelector(".viewer-progress");
        var viewerContent = viewer.querySelector(".viewer-content");
        var viewerMeta = viewer.querySelector(".viewer-meta");
        var viewerCloseBtn = document.getElementById("viewer-close-btn");

        var currentEntryIdx = 0;
        var currentPageIdx = 0;
        var entryPages = []; // array of arrays: each entry's pages (photos + text)

        // Build entry pages from the DOM
        mobileCards.forEach(function (card, idx) {
            var pages = [];
            var entryEl = document.querySelectorAll(".timeline-entry")[idx];
            if (!entryEl) return;

            // Get photos from the desktop timeline entry
            var photos = entryEl.querySelectorAll(".timeline-photos img, .timeline-photos video");
            photos.forEach(function (el) {
                pages.push({
                    type: el.tagName === "VIDEO" ? "video" : "photo",
                    src: el.dataset.full || el.src
                });
            });

            // Get text
            var bodyEl = entryEl.querySelector(".timeline-body");
            if (bodyEl && bodyEl.textContent.trim()) {
                pages.push({
                    type: "text",
                    content: bodyEl.textContent.trim()
                });
            }

            // If no pages at all, add a text placeholder
            if (pages.length === 0) {
                pages.push({ type: "text", content: "" });
            }

            // Get metadata
            var lat = card.dataset.lat;
            var lon = card.dataset.lon;
            var headerEl = entryEl.querySelector(".timeline-entry-header");
            var dateEl = entryEl.querySelector(".timeline-date-day");
            var timeEl = entryEl.querySelector(".timeline-date-time");
            var distLabel = entryEl.querySelector(".timeline-distance-label");
            var meta = {
                lat: lat,
                lon: lon,
                header: headerEl ? headerEl.textContent.trim() : "",
                date: dateEl ? dateEl.textContent.trim() : "",
                time: timeEl ? timeEl.textContent.trim() : "",
                dist: distLabel ? distLabel.textContent.trim() : ""
            };

            var distKm = parseFloat(card.dataset.dist) || 0;
            entryPages.push({ pages: pages, meta: meta, distFromPrev: distKm });
        });

        // Compute cumulative distances
        var cumulativeDist = 0;
        entryPages.forEach(function (entry) {
            cumulativeDist += entry.distFromPrev;
            entry.meta.totalKm = Math.round(cumulativeDist * 10) / 10;
        });

        // Open viewer for an entry
        function openViewer(entryIdx) {
            currentEntryIdx = entryIdx;
            currentPageIdx = 0;
            renderViewer();
            viewer.classList.add("active");

            // Fly map to entry
            var card = mobileCards[entryIdx];
            if (card && card.dataset.lat) {
                map.flyTo({
                    center: [parseFloat(card.dataset.lon), parseFloat(card.dataset.lat)],
                    zoom: 12,
                    duration: 500
                });
            }
        }

        var autoAdvanceTimer = null;
        var autoAdvancePaused = false;
        var PHOTO_DURATION = 5000; // 5 seconds per photo
        var TEXT_DURATION = 8000;  // 8 seconds for text

        function stopAutoAdvance() {
            if (autoAdvanceTimer) {
                clearTimeout(autoAdvanceTimer);
                autoAdvanceTimer = null;
            }
            // Stop CSS animations
            var bars = viewerProgress.querySelectorAll(".viewer-progress-fill");
            bars.forEach(function (b) { b.style.animationPlayState = "paused"; });
        }

        function startAutoAdvance() {
            stopAutoAdvance();
            if (autoAdvancePaused) return;

            var entry = entryPages[currentEntryIdx];
            if (!entry) return;
            var page = entry.pages[currentPageIdx];
            var duration = page.type === "text" ? TEXT_DURATION : PHOTO_DURATION;

            // Resume CSS animation on current bar
            var currentBar = viewerProgress.querySelectorAll(".viewer-progress-bar")[currentPageIdx];
            if (currentBar) {
                var fill = currentBar.querySelector(".viewer-progress-fill");
                if (fill) fill.style.animationPlayState = "running";
            }

            autoAdvanceTimer = setTimeout(function () {
                if (currentPageIdx < entry.pages.length - 1) {
                    currentPageIdx++;
                    renderViewer();
                } else {
                    closeViewer();
                    scrollToCard(currentEntryIdx);
                }
            }, duration);
        }

        function closeViewer() {
            stopAutoAdvance();
            viewer.classList.remove("active");
        }

        function renderViewer() {
            var entry = entryPages[currentEntryIdx];
            if (!entry) return;

            var page = entry.pages[currentPageIdx];
            var duration = page.type === "text" ? TEXT_DURATION : PHOTO_DURATION;

            // Top info
            var topDate = viewer.querySelector(".viewer-top-date");
            var topKm = viewer.querySelector(".viewer-top-km");
            topDate.textContent = entry.meta.date + (entry.meta.time ? " · " + entry.meta.time : "");
            topKm.textContent = entry.meta.totalKm > 0 ? entry.meta.totalKm + " km" : "";

            // Progress bars with fill animation
            viewerProgress.innerHTML = "";
            entry.pages.forEach(function (_, i) {
                var bar = document.createElement("div");
                bar.className = "viewer-progress-bar";
                var fill = document.createElement("div");
                fill.className = "viewer-progress-fill";
                if (i < currentPageIdx) {
                    fill.style.width = "100%";
                    fill.style.animation = "none";
                } else if (i === currentPageIdx) {
                    fill.style.animation = "progress-fill " + (duration / 1000) + "s linear forwards";
                }
                bar.appendChild(fill);
                viewerProgress.appendChild(bar);
            });

            // Start auto-advance
            startAutoAdvance();

            // Content
            viewerContent.innerHTML = "";
            if (page.type === "photo") {
                var img = document.createElement("img");
                img.src = page.src;
                viewerContent.appendChild(img);
            } else if (page.type === "video") {
                var vid = document.createElement("video");
                vid.src = page.src;
                vid.controls = true;
                vid.autoplay = true;
                viewerContent.appendChild(vid);
                stopAutoAdvance(); // Let video play at its own pace
            } else {
                var txt = document.createElement("div");
                txt.className = "viewer-text";
                txt.textContent = page.content || "No text";
                viewerContent.appendChild(txt);
            }

            // Meta
            viewerMeta.innerHTML = "";
            if (entry.meta.header) {
                var info = document.createElement("div");
                info.className = "viewer-meta-info";
                info.textContent = entry.meta.header;
                viewerMeta.appendChild(info);
            }
        }

        // Tap left/right on viewer content
        viewerContent.addEventListener("click", function (e) {
            var rect = viewerContent.getBoundingClientRect();
            var x = e.clientX - rect.left;
            var entry = entryPages[currentEntryIdx];

            if (x < rect.width * 0.3) {
                // Tap left — previous page or previous entry
                if (currentPageIdx > 0) {
                    currentPageIdx--;
                    renderViewer();
                } else if (currentEntryIdx > 0) {
                    currentEntryIdx--;
                    currentPageIdx = entryPages[currentEntryIdx].pages.length - 1;
                    renderViewer();
                    scrollToCard(currentEntryIdx);
                }
            } else if (x > rect.width * 0.7) {
                // Tap right — next page or close viewer
                if (currentPageIdx < entry.pages.length - 1) {
                    currentPageIdx++;
                    renderViewer();
                } else {
                    closeViewer();
                    scrollToCard(currentEntryIdx);
                }
            }
        });

        // Hold to pause auto-advance
        var holdTimer = null;
        var isHolding = false;

        viewerContent.addEventListener("touchstart", function () {
            holdTimer = setTimeout(function () {
                isHolding = true;
                autoAdvancePaused = true;
                stopAutoAdvance();
            }, 200);
        }, { passive: true });

        viewerContent.addEventListener("touchend", function () {
            clearTimeout(holdTimer);
            if (isHolding) {
                isHolding = false;
                autoAdvancePaused = false;
                startAutoAdvance();
            }
        });

        viewerContent.addEventListener("mousedown", function () {
            holdTimer = setTimeout(function () {
                isHolding = true;
                autoAdvancePaused = true;
                stopAutoAdvance();
            }, 200);
        });

        document.addEventListener("mouseup", function () {
            clearTimeout(holdTimer);
            if (isHolding) {
                isHolding = false;
                autoAdvancePaused = false;
                startAutoAdvance();
            }
        });

        // Close button closes viewer
        viewerCloseBtn.addEventListener("click", function () { closeViewer(); });

        // Swipe down closes viewer
        var touchStartY = 0;
        var touchDeltaY = 0;
        viewer.addEventListener("touchstart", function (e) {
            touchStartY = e.touches[0].clientY;
            touchDeltaY = 0;
        }, { passive: true });

        viewer.addEventListener("touchmove", function (e) {
            touchDeltaY = e.touches[0].clientY - touchStartY;
            if (touchDeltaY > 0) {
                viewer.style.transform = "translateY(" + touchDeltaY + "px)";
                viewer.style.opacity = Math.max(0.3, 1 - touchDeltaY / 300);
            }
        }, { passive: true });

        viewer.addEventListener("touchend", function () {
            if (touchDeltaY > 100) {
                closeViewer();
            }
            viewer.style.transform = "";
            viewer.style.opacity = "";
        });

        // Tap on mobile entry cards to open viewer
        mobileCards.forEach(function (card, idx) {
            card.addEventListener("click", function () {
                openViewer(idx);
            });
        });

        // Scroll horizontal timeline to active card
        function scrollToCard(idx) {
            var card = mobileCards[idx];
            if (card) {
                card.scrollIntoView({ behavior: "smooth", inline: "center", block: "nearest" });

                // Fly map
                if (card.dataset.lat) {
                    map.flyTo({
                        center: [parseFloat(card.dataset.lon), parseFloat(card.dataset.lat)],
                        zoom: 12,
                        duration: 500
                    });
                }
            }
        }

        // Highlight active card in timeline
        function highlightCard(idx) {
            mobileCards.forEach(function (c) { c.classList.remove("mobile-card-active"); });
            if (mobileCards[idx]) mobileCards[idx].classList.add("mobile-card-active");
        }

        // Progress bar + badge
        var progressBarFill = document.querySelector(".mobile-progress-bar-fill");
        var progressBadge = document.querySelector(".mobile-progress-badge");
        var progressBarWrap = document.querySelector(".mobile-progress-bar-wrap");

        // Detect which card is centered during scroll
        var trackEl = document.querySelector(".mobile-timeline-track");
        var scrollDebounce;
        trackEl.addEventListener("scroll", function () {
            clearTimeout(scrollDebounce);
            scrollDebounce = setTimeout(function () {
                if (badgeDragging) return;
                var trackRect = trackEl.getBoundingClientRect();
                var centerX = trackRect.left + trackRect.width / 2;
                var closest = 0;
                var closestDist = Infinity;
                mobileCards.forEach(function (card, idx) {
                    var cardRect = card.getBoundingClientRect();
                    var cardCenter = cardRect.left + cardRect.width / 2;
                    var dist = Math.abs(cardCenter - centerX);
                    if (dist < closestDist) {
                        closestDist = dist;
                        closest = idx;
                    }
                });
                highlightCard(closest);

                // Fly map to this entry
                var card = mobileCards[closest];
                if (card && card.dataset.lat) {
                    map.flyTo({
                        center: [parseFloat(card.dataset.lon), parseFloat(card.dataset.lat)],
                        zoom: Math.max(map.getZoom(), 10),
                        duration: 500
                    });
                }
            }, 30);
        });

        // Position dots to match card centers and sync scroll
        var progressLine = document.querySelector(".mobile-progress-line");

        function updateProgressBar() {
            if (!trackEl || !progressBarFill || !progressBadge) return;
            var maxScroll = trackEl.scrollWidth - trackEl.clientWidth;
            var scrollRatio = maxScroll > 0 ? trackEl.scrollLeft / maxScroll : 0;
            var pct = Math.min(100, scrollRatio * 100);
            progressBarFill.style.width = pct + "%";

            // Position badge — ensure it stays visible
            if (progressBarWrap) {
                var barWidth = progressBarWrap.clientWidth;
                var badgeWidth = progressBadge.offsetWidth;
                var pos = Math.max(badgeWidth, pct / 100 * barWidth);
                progressBadge.style.left = pos + "px";
            }

            // Update badge text with cumulative km
            var closest = 0;
            var trackRect = trackEl.getBoundingClientRect();
            var centerX = trackRect.left + trackRect.width * 0.3;
            var closestDist = Infinity;
            mobileCards.forEach(function (card, idx) {
                var cardRect = card.getBoundingClientRect();
                var cardCenter = cardRect.left + cardRect.width / 2;
                var dist = Math.abs(cardCenter - centerX);
                if (dist < closestDist) { closestDist = dist; closest = idx; }
            });

            if (entryPages[closest] && entryPages[closest].meta.totalKm !== undefined) {
                progressBadge.textContent = entryPages[closest].meta.totalKm + " km";
            }
        }

        trackEl.addEventListener("scroll", updateProgressBar);

        // Focus last card for visitors, or scroll to "New Entry" for owners
        var initialIdx = mobileCards.length - 1;
        if (typeof isOwner !== "undefined" && isOwner) {
            // Scroll to the very end (past last card, to the "+ New Entry" link)
            trackEl.scrollLeft = trackEl.scrollWidth;
        } else if (initialIdx >= 0) {
            mobileCards[initialIdx].scrollIntoView({ inline: "center", block: "nearest" });
        }
        updateProgressBar();
        highlightCard(Math.max(0, initialIdx));

        // Draggable progress badge
        var badgeDragging = false;
        progressBadge.addEventListener("touchstart", function (e) {
            badgeDragging = true;
            trackEl.style.scrollSnapType = "none";
            progressBadge.style.transition = "none";
            progressBarFill.style.transition = "none";
            e.preventDefault();
        });
        progressBadge.addEventListener("mousedown", function (e) {
            badgeDragging = true;
            trackEl.style.scrollSnapType = "none";
            progressBadge.style.transition = "none";
            progressBarFill.style.transition = "none";
            e.preventDefault();
        });

        function handleBadgeDrag(clientX) {
            if (!badgeDragging || !progressBarWrap) return;
            var barRect = progressBarWrap.getBoundingClientRect();
            var pct = Math.max(0, Math.min(1, (clientX - barRect.left) / barRect.width));
            var maxScroll = trackEl.scrollWidth - trackEl.clientWidth;
            trackEl.scrollLeft = pct * maxScroll;
        }

        function stopBadgeDrag() {
            if (!badgeDragging) return;
            badgeDragging = false;
            trackEl.style.scrollSnapType = "";
            progressBadge.style.transition = "";
            progressBarFill.style.transition = "";
        }

        document.addEventListener("touchmove", function (e) {
            if (badgeDragging) handleBadgeDrag(e.touches[0].clientX);
        });
        document.addEventListener("mousemove", function (e) {
            if (badgeDragging) handleBadgeDrag(e.clientX);
        });
        document.addEventListener("touchend", stopBadgeDrag);
        document.addEventListener("mouseup", stopBadgeDrag);

        // Initialize first dot as active
        updateProgressDot(0);
    }
});
