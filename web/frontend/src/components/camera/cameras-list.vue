<script setup lang="ts">
import type {
  commonApi,
  Client,
  ResponseStream,
  robotApi,
} from '@viamrobotics/sdk';

import Camera from './camera.vue';
import PCD from '../pcd/pcd.vue';
import { selectedMap } from '../../lib/camera-state';
import type { StreamManager } from './stream-manager';
import { $ref } from '@vue-macros/reactivity-transform/macros';

const props = defineProps<{
  resources: commonApi.ResourceName.AsObject[],
  streamManager: StreamManager,
  client: Client,
  parentName: string
  statusStream: ResponseStream<robotApi.StreamStatusResponse> | null
}>();

const openCameras = $ref<Record<string, boolean | undefined>>({});
const refreshFrequency = $ref<Record<string, string | undefined>>({});
const triggerRefresh = $ref(false);

const setupCamera = (cameraName: string) => {
  openCameras[cameraName] = !openCameras[cameraName];
  for (const camera of props.resources) {
    if (!refreshFrequency[camera.name]) {
      refreshFrequency[camera.name] = 'Live';
    }
  }
};

</script>

<template>
  <v-collapse
    v-for="camera in resources"
    :key="camera.name"
    :title="camera.name"
    class="camera"
    data-parent="app"
  >
    <v-breadcrumbs
      slot="title"
      crumbs="camera"
    />

    <div class="flex flex-col gap-4 border border-t-0 border-medium p-4">
      <v-switch
        :label="camera.name"
        :aria-label="openCameras[camera.name] ? `Hide Camera: ${camera.name}` : `View Camera: ${camera.name}`"
        :value="openCameras[camera.name] ? 'on' : 'off'"
        @input="setupCamera(camera.name)"
      />

      <div
        v-if="openCameras[camera.name]"
        class="flex flex-wrap items-end gap-2"
      >
        <v-select
          v-model="refreshFrequency[camera.name]"
          class="w-fit"
          label="Refresh frequency"
          aria-label="Refresh frequency"
          :options="Object.keys(selectedMap).join(',')"
        />

        <v-button
          v-if="refreshFrequency[camera.name] !== 'Live'"
          icon="refresh"
          label="Refresh"
          @click="triggerRefresh = !triggerRefresh"
        />
      </div>

      <Camera
        v-if="openCameras[camera.name]"
        :key="camera.name"
        :camera-name="camera.name"
        :parent-name="parentName"
        :client="client"
        :resources="resources"
        :show-export-screenshot="true"
        :refresh-rate="refreshFrequency[camera.name]"
        :trigger-refresh="triggerRefresh"
        :stream-manager="props.streamManager"
        :status-stream="props.statusStream"
      />

      <PCD
        :key="camera.name"
        :camera-name="camera.name"
        :parent-name="parentName"
        :client="client"
        :resources="resources"
        :show-switch="true"
        :show-refresh="true"
      />
    </div>
  </v-collapse>
</template>
