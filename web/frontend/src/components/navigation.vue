<script setup lang="ts">

import { $ref } from '@vue-macros/reactivity-transform/macros';
import { onMounted, onUnmounted } from 'vue';
import { grpc } from '@improbable-eng/grpc-web';
import { toast } from '../lib/toast';
import { filterResources } from '../lib/resource';
import { Client, commonApi, robotApi, navigationApi, type ServiceError, type ResponseStream } from '@viamrobotics/sdk';
import { Struct } from 'google-protobuf/google/protobuf/struct_pb';
import { rcLogConditionally } from '../lib/log';

const props = defineProps<{
  resources: commonApi.ResourceName.AsObject[]
  name:string
  client: Client
  statusStream: ResponseStream<robotApi.StreamStatusResponse> | null
}>();

let googleMapsInitResolve: () => void;
const mapReady = new Promise<void>((resolve) => {
  googleMapsInitResolve = resolve;
});

let map: google.maps.Map;
let updateWaypointsId: number;
let updateLocationsId: number;

let mapInit = $ref(false);
let googleApiKey = $ref('');
const location = $ref('');
const container = $ref<HTMLElement>();

const grpcCallback = (error: ServiceError | null) => {
  if (error) {
    toast.error(error.message);
  }
};

const setNavigationMode = (mode: 'manual' | 'waypoint') => {
  let pbMode: 0 | 1 | 2 = navigationApi.Mode.MODE_UNSPECIFIED;

  if (mode === 'manual') {
    pbMode = navigationApi.Mode.MODE_MANUAL;
  } else if (mode === 'waypoint') {
    pbMode = navigationApi.Mode.MODE_WAYPOINT;
  }

  const req = new navigationApi.SetModeRequest();
  req.setName(props.name);
  req.setMode(pbMode);

  rcLogConditionally(req);
  props.client.navigationService.setMode(req, new grpc.Metadata(), grpcCallback);
};

const setNavigationLocation = () => {
  const [latStr, lngStr] = location.split(',');
  if (latStr === undefined || lngStr === undefined) {
    return;
  }

  const lat = Number.parseFloat(latStr);
  const lng = Number.parseFloat(lngStr);

  // TODO: Not sure how this works (if it does), robotApi has no ResourceRunCommandRequest method
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const req = new (robotApi as any).ResourceRunCommandRequest();
  let gpsName = '';

  const [gps] = filterResources(props.resources ?? [], 'rdk', 'component', 'gps');

  if (gps) {
    gpsName = gps.name;
  } else {
    toast.error('no gps device found');
    return;
  }

  req.setName(props.name);
  req.setResourceName(gpsName);
  req.setCommandName('set_location');
  req.setArgs(
    Struct.fromJavaScript({
      latitude: lat,
      longitude: lng,
    })
  );

  rcLogConditionally(req);
  props.client.genericService.doCommand(req, new grpc.Metadata(), grpcCallback);
};

const initNavigation = async () => {
  await mapReady;

  map = new window.google.maps.Map(container!, { zoom: 18 });
  map.addListener('click', (event: google.maps.MapMouseEvent) => {
    const lat = event.latLng?.lat();
    const lng = event.latLng?.lng();

    if (lat === undefined || lng === undefined) {
      return;
    }

    const req = new navigationApi.AddWaypointRequest();
    const point = new commonApi.GeoPoint();

    point.setLatitude(lat);
    point.setLongitude(lng);
    req.setName(props.name);
    req.setLocation(point);

    rcLogConditionally(req);
    props.client.navigationService.addWaypoint(req, new grpc.Metadata(), grpcCallback);
  });

  let centered = false;
  const knownWaypoints: Record<string, google.maps.Marker> = {};
  let localLabelCounter = 0;

  const updateWaypoints = () => {
    const req = new navigationApi.GetWaypointsRequest();
    req.setName(props.name);

    rcLogConditionally(req);
    props.client.navigationService.getWaypoints(
      req,
      new grpc.Metadata(),
      (err: ServiceError | null, resp: navigationApi.GetWaypointsResponse | null) => {
        grpcCallback(err);

        if (err) {
          updateWaypointsId = window.setTimeout(updateWaypoints, 1000);
          return;
        }

        let waypoints: navigationApi.Waypoint[] = [];

        if (resp) {
          waypoints = resp.getWaypointsList();
        }

        const currentWaypoints: Record<string, google.maps.Marker> = {};

        for (const waypoint of waypoints) {
          const pos = {
            lat: waypoint.getLocation()?.getLatitude() ?? 0,
            lng: waypoint.getLocation()?.getLongitude() ?? 0,
          };

          const posStr = JSON.stringify(pos);

          if (knownWaypoints[posStr]) {
            currentWaypoints[posStr] = knownWaypoints[posStr]!;
            continue;
          }

          localLabelCounter += 1;

          const marker = new window.google.maps.Marker({
            position: pos,
            map,
            label: `${localLabelCounter}`,
          });

          currentWaypoints[posStr] = marker;
          knownWaypoints[posStr] = marker;

          marker.addListener('click', () => {
            console.debug('clicked on marker', pos);
          });

          marker.addListener('dblclick', () => {
            const waypointRequest = new navigationApi.RemoveWaypointRequest();
            waypointRequest.setName(props.name);
            waypointRequest.setId(waypoint.getId());

            rcLogConditionally(req);
            props.client.navigationService.removeWaypoint(
              waypointRequest,
              new grpc.Metadata(),
              grpcCallback
            );
          });
        }

        const waypointsToDelete = Object.keys(knownWaypoints).filter(
          (elem) => !(elem in currentWaypoints)
        );

        for (const key of waypointsToDelete) {
          const marker = knownWaypoints[key]!;
          marker.setMap(null);
          delete knownWaypoints[key];
        }

        updateWaypointsId = window.setTimeout(updateWaypoints, 1000);
      }
    );
  };

  updateWaypoints();

  const locationMarker = new window.google.maps.Marker({ label: 'robot' });

  const updateLocation = () => {
    const req = new navigationApi.GetLocationRequest();
    req.setName(props.name);

    rcLogConditionally(req);
    props.client.navigationService.getLocation(
      req,
      new grpc.Metadata(),
      (err: ServiceError | null, resp: navigationApi.GetLocationResponse | null) => {
        grpcCallback(err);

        if (err) {
          updateLocationsId = window.setTimeout(updateLocation, 1000);
          return;
        }

        const pos = {
          lat: resp?.getLocation()?.getLatitude() ?? 0,
          lng: resp?.getLocation()?.getLongitude() ?? 0,
        };

        if (!centered) {
          centered = true;
          map.setCenter(pos);
        }

        locationMarker.setPosition(pos);
        locationMarker.setMap(map);

        updateLocationsId = window.setTimeout(updateLocation, 1000);
      }
    );
  };

  updateLocation();
};

const loadMaps = () => {
  if (document.querySelector('#google-maps')) {
    initNavigation();
    return;
  }

  const script = document.createElement('script');
  script.id = 'google-maps';
  script.src = `https://maps.googleapis.com/maps/api/js?key=${googleApiKey}` +
    '&callback=googleMapsInit&libraries=&v=weekly&map_ids=google-maps-1';
  script.async = true;
  document.head.append(script);
};

window.googleMapsInit = () => {
  console.debug('google maps is ready');
  googleMapsInitResolve();
};

const initNavigationView = () => {
  window.localStorage.setItem('nav-svc-google-api-key', googleApiKey);
  mapInit = true;
  loadMaps();
  initNavigation();
};

onMounted(() => {
  const apiKey = window.localStorage.getItem('nav-svc-google-api-key');
  if (apiKey) {
    googleApiKey = apiKey;
    initNavigationView();
  }

  props.statusStream?.on('end', () => {
    clearTimeout(updateWaypointsId);
    clearTimeout(updateLocationsId);
  });
});

onUnmounted(() => {
  clearTimeout(updateWaypointsId);
  clearTimeout(updateLocationsId);
});

</script>

<template>
  <v-collapse
    :title="props.name"
    class="navigation"
  >
    <v-breadcrumbs
      slot="title"
      crumbs="navigation"
    />
    <div class="flex flex-col gap-2 border border-t-0 border-medium p-4">
      <div class="flex h-full w-full flex-row items-end gap-2">
        <v-input
          label="Google Maps API Key"
          :value="googleApiKey"
          @input="googleApiKey = $event.detail.value"
        />
        <div class="flex h-[30px]">
          <v-button
            label="Go"
            @click="initNavigationView"
          />
        </div>
      </div>
      <div v-show="mapInit">
        <v-radio
          label="Navigation mode"
          options="Manual, Waypoint"
          @input="setNavigationMode($event.detail.value.toLowerCase())"
        />

        <div>
          <v-button
            label="Try Set Location"
            @click="setNavigationLocation()"
          />
        </div>

        <div
          id="google-maps-1"
          ref="container"
          class="mb-2 h-[400px] w-full"
        />

        <v-input
          label="Location"
          :value="location"
          @input="location = $event.detail.value"
        />
      </div>
    </div>
  </v-collapse>
</template>
