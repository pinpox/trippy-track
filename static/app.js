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
                        "line-color": "#2563eb",
                        "line-width": 3,
                    },
                });

                // Add entry markers
                var entryFeatures = geojson.features.filter(function (f) {
                    return f.properties.type === "entry";
                });

                entryFeatures.forEach(function (f) {
                    var el = document.createElement("div");
                    el.className = "map-marker";

                    var marker = new maplibregl.Marker({ element: el })
                        .setLngLat(f.geometry.coordinates)
                        .addTo(map);

                    // Click marker -> scroll to entry
                    el.addEventListener("click", function () {
                        var entryEl = document.querySelector(
                            '[data-entry-id="' + f.properties.id + '"]'
                        );
                        if (entryEl) {
                            entryEl.scrollIntoView({
                                behavior: "smooth",
                                block: "center",
                            });
                            highlightEntry(entryEl);
                        }
                    });

                    markers.push({
                        marker: marker,
                        entryId: f.properties.id,
                    });
                });

                // Fit bounds to track + markers
                fitMapBounds(geojson);
            });
    });

    // Scroll timeline -> pan map
    var observer = new IntersectionObserver(
        function (observerEntries) {
            observerEntries.forEach(function (oe) {
                if (oe.isIntersecting) {
                    var lat = parseFloat(oe.target.dataset.lat);
                    var lon = parseFloat(oe.target.dataset.lon);
                    if (!isNaN(lat) && !isNaN(lon)) {
                        map.flyTo({
                            center: [lon, lat],
                            zoom: Math.max(map.getZoom(), 10),
                            duration: 500,
                        });
                    }
                }
            });
        },
        {
            root: document.getElementById("timeline"),
            threshold: 0.5,
        }
    );

    document.querySelectorAll(".timeline-entry[data-lat]").forEach(function (el) {
        observer.observe(el);
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
        '<img class="lightbox-img" src="">' +
        '<button class="lightbox-prev">&lsaquo;</button>' +
        '<button class="lightbox-next">&rsaquo;</button>' +
        '<button class="lightbox-close">&times;</button>' +
        '<span class="lightbox-counter"></span>';
    document.body.appendChild(lightbox);

    var lightboxImg = lightbox.querySelector(".lightbox-img");
    var lightboxCounter = lightbox.querySelector(".lightbox-counter");
    var currentPhotos = [];
    var currentIndex = 0;

    function openLightbox(img) {
        var container = img.closest(".timeline-photos");
        if (!container) return;
        // Include all photos, even hidden ones
        currentPhotos = Array.from(container.querySelectorAll("img"));
        currentIndex = currentPhotos.indexOf(img);
        if (currentIndex === -1) currentIndex = 0;
        showPhoto();
        lightbox.classList.add("active");
    }

    function showPhoto() {
        lightboxImg.src = currentPhotos[currentIndex].src;
        lightboxCounter.textContent = (currentIndex + 1) + " / " + currentPhotos.length;
    }

    function closeLightbox() {
        lightbox.classList.remove("active");
    }

    lightbox.querySelector(".lightbox-backdrop").addEventListener("click", closeLightbox);
    lightbox.querySelector(".lightbox-close").addEventListener("click", closeLightbox);

    lightbox.querySelector(".lightbox-prev").addEventListener("click", function (e) {
        e.stopPropagation();
        currentIndex = (currentIndex - 1 + currentPhotos.length) % currentPhotos.length;
        showPhoto();
    });

    lightbox.querySelector(".lightbox-next").addEventListener("click", function (e) {
        e.stopPropagation();
        currentIndex = (currentIndex + 1) % currentPhotos.length;
        showPhoto();
    });

    document.addEventListener("keydown", function (e) {
        if (!lightbox.classList.contains("active")) return;
        if (e.key === "Escape") closeLightbox();
        if (e.key === "ArrowLeft") {
            currentIndex = (currentIndex - 1 + currentPhotos.length) % currentPhotos.length;
            showPhoto();
        }
        if (e.key === "ArrowRight") {
            currentIndex = (currentIndex + 1) % currentPhotos.length;
            showPhoto();
        }
    });

    document.querySelectorAll(".timeline-photos img").forEach(function (img) {
        img.style.cursor = "pointer";
        img.addEventListener("click", function () {
            openLightbox(img);
        });
    });

    document.querySelectorAll(".photos-more").forEach(function (el) {
        el.addEventListener("click", function () {
            // Open lightbox at the 4th photo (first hidden one)
            var container = el.closest(".timeline-photos");
            var imgs = Array.from(container.querySelectorAll("img"));
            if (imgs.length > 3) {
                openLightbox(imgs[3]);
            }
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
            map.fitBounds(bounds, { padding: 50 });
        }
    }
});
