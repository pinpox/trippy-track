document.addEventListener("DOMContentLoaded", function () {
    const map = new maplibregl.Map({
        container: "map",
        style: "https://tiles.openfreemap.org/styles/liberty",
        center: [10, 50],
        zoom: 4,
    });

    map.addControl(new maplibregl.NavigationControl());

    const markers = [];

    map.on("load", function () {
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
