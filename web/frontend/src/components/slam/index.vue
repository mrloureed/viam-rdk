<script setup lang="ts">

import { $ref, $computed } from '@vue-macros/reactivity-transform/macros';
import { grpc } from '@improbable-eng/grpc-web';
import { toast } from '@/lib/toast';
import { Struct } from 'google-protobuf/google/protobuf/struct_pb';
import * as THREE from 'three';
import {
  Client,
  commonApi,
  type ResponseStream,
  robotApi,
  type ServiceError,
  slamApi,
  motionApi,
} from '@viamrobotics/sdk';
import { displayError, isServiceError } from '@/lib/error';
import { rcLogConditionally } from '@/lib/log';
import PCD from '../pcd/pcd-view.vue';
import { copyToClipboardWithToast } from '@/lib/copy-to-clipboard';
import Slam2dRenderer from './2d-renderer.vue';
import { filterResources } from '@/lib/resource';
import { onMounted, onUnmounted } from 'vue';

type MapAndPose = { map: Uint8Array, pose: commonApi.Pose}

const props = defineProps<{
  name: string
  resources: commonApi.ResourceName.AsObject[]
  client: Client
  statusStream: ResponseStream<robotApi.StreamStatusResponse> | null
  operations: {
    op: robotApi.Operation.AsObject
    elapsed: number
  }[]
}>();

const refreshErrorMessage = 'Error refreshing map. The map shown may be stale.';
const displayPose = $ref({ x: 0, y: 0, z: 0, ox: 0, oy: 0, oz: 0, th: 0 });
let refreshErrorMessage2d = $ref<string | null>();
let refreshErrorMessage3d = $ref<string | null>();
let selected2dValue = $ref('manual');
let selected3dValue = $ref('manual');
let pointCloudUpdateCount = $ref(0);
let pointcloud = $ref<Uint8Array | undefined>();
let pose = $ref<commonApi.Pose | undefined>();
let show2d = $ref(false);
let show3d = $ref(false);
let showAxes = $ref(true);
let refresh2DCancelled = true;
let refresh3DCancelled = true;
let updatedDest = $ref(false);
let destinationMarker = $ref(new THREE.Vector3());

const motionServiceReq = new motionApi.MoveOnMapRequest();

const loaded2d = $computed(() => (pointcloud !== undefined && pose !== undefined));

let slam2dTimeoutId = -1;
let slam3dTimeoutId = -1;

const moveClicked = $computed(() => {
  for (const element of props.operations) {
    if (element.op.method.includes('MoveOnMap')) {
      return true;
    }
  }
  return false;
});

// get all resources which are bases
const baseResources = $computed(() => filterResources(props.resources, 'rdk', 'component', 'base'));

// allowMove is only true if we have a base, there exists a destination and there is no in-flight MoveOnMap req
const allowMove = $computed(() => (
  baseResources !== undefined &&
  baseResources.length === 1 &&
  updatedDest &&
  !moveClicked
));

const concatArrayU8 = (arrays: Uint8Array[]) => {
  const totalLength = arrays.reduce((acc, value) => acc + value.length, 0);
  const result = new Uint8Array(totalLength);
  let length = 0;
  for (const array of arrays) {
    result.set(array, length);
    length += array.length;
  }
  return result;
};

const fetchSLAMMap = (name: string): Promise<Uint8Array> => {
  return new Promise((resolve, reject) => {
    const req = new slamApi.GetPointCloudMapRequest();
    req.setName(name);
    rcLogConditionally(req);
    const chunks: Uint8Array[] = [];
    const getPointCloudMap: ResponseStream<slamApi.GetPointCloudMapResponse> =
      props.client.slamService.getPointCloudMap(req);
    getPointCloudMap.on('data', (res: { getPointCloudPcdChunk_asU8(): Uint8Array }) => {
      const chunk = res.getPointCloudPcdChunk_asU8();
      chunks.push(chunk);
    });
    getPointCloudMap.on('status', (status: { code: number, details: string, metadata: grpc.Metadata }) => {
      if (status.code !== 0) {
        const error = {
          message: status.details,
          code: status.code,
          metadata: status.metadata,
        };
        reject(error);
      }
    });
    getPointCloudMap.on('end', (end?: { code: number, details: string, metadata: grpc.Metadata }) => {
      if (end === undefined) {
        const error = { message: 'Stream ended without status code' };
        reject(error);
      } else if (end.code !== 0) {
        const error = {
          message: end.details,
          code: end.code,
          metadata: end.metadata,
        };
        reject(error);
      }
      const arr = concatArrayU8(chunks);
      resolve(arr);
    });
  });
};

const fetchSLAMPose = (name: string): Promise<commonApi.Pose> => {
  return new Promise((resolve, reject): void => {
    const req = new slamApi.GetPositionRequest();
    req.setName(name);
    props.client.slamService.getPosition(
      req,
      new grpc.Metadata(),
      (error: ServiceError | null, res: slamApi.GetPositionResponse | null): void => {
        if (error) {
          reject(error);
          return;
        }
        resolve(res!.getPose()!);
      }
    );
  });
};

const fetchFeatureFlags = (name: string): Promise<{[key: string]: boolean}> => {
  return new Promise((resolve, reject): void => {
    const request = new commonApi.DoCommandRequest();
    request.setName(name);
    request.setCommand(Struct.fromJavaScript({ feature_flag: true }));
    props.client.slamService.doCommand(
      request,
      new grpc.Metadata(),
      (error: ServiceError|null, responseMessage: commonApi.DoCommandResponse|null) => {

        /*
         * Note: we ignore unimplementedError because in the current implementation it
         *  signifies that the feature flag is false
         */
        if (error) {
          if (error.code === grpc.Code.Unimplemented || error.code === grpc.Code.Unknown) {
            resolve({});
            return;
          }
          reject(error);
          return;

        }
        resolve(responseMessage!.getResult()?.toJavaScript() as {[key: string]: boolean});
      }
    );
  });
};

const deleteDestinationMarker = () => {
  updatedDest = false;
  destinationMarker = new THREE.Vector3();
};

const moveOnMap = async () => {

  /*
   * set request name
   * here we set the name of the motion service the user is using
   */
  motionServiceReq.setName('builtin');

  // set pose in frame
  const destination = new commonApi.Pose();
  const value = await fetchSLAMPose(props.name);
  destination.setX(destinationMarker.x);
  destination.setY(destinationMarker.y);
  destination.setZ(destinationMarker.z);
  destination.setOX(value.getOX());
  destination.setOY(value.getOY());
  destination.setOZ(value.getOZ());
  destination.setTheta(value.getTheta());
  motionServiceReq.setDestination(destination);

  // set SLAM resource name
  const slamResourceName = new commonApi.ResourceName();
  slamResourceName.setNamespace('rdk');
  slamResourceName.setType('service');
  slamResourceName.setSubtype('slam');
  slamResourceName.setName(props.name);
  motionServiceReq.setSlamServiceName(slamResourceName);

  // set component name
  const baseResourceName = new commonApi.ResourceName();
  baseResourceName.setNamespace('rdk');
  baseResourceName.setType('component');
  baseResourceName.setSubtype('base');
  baseResourceName.setName(baseResources[0]!.name);
  motionServiceReq.setComponentName(baseResourceName);

  // set extra as position-only constraint
  motionServiceReq.setExtra(
    Struct.fromJavaScript({
      motion_profile: 'position_only',
    })
  );

  props.client.motionService.moveOnMap(
    motionServiceReq,
    new grpc.Metadata(),
    (error: ServiceError | null, response: motionApi.MoveOnMapResponse | null) => {
      if (error) {
        deleteDestinationMarker();
        toast.error(`Error moving: ${error}`);
        return;
      }
      deleteDestinationMarker();
      toast.success(`MoveOnMap success: ${response!.getSuccess()}`);
    }
  );

};

const stopMoveOnMap = () => {
  for (const element of props.operations) {
    if (element.op.method.includes('MoveOnMap')) {
      const req = new robotApi.CancelOperationRequest();
      req.setId(element.op.id);
      rcLogConditionally(req);
      props.client.robotService.cancelOperation(req, new grpc.Metadata(), displayError);
    }
  }
};

const refresh2d = async (name: string) => {
  const flags = await fetchFeatureFlags(name);

  const map = await fetchSLAMMap(name);
  const returnedPose = await fetchSLAMPose(name);

  // TODO: Remove this check when APP and carto are both up to date [RSDK-3166]
  if (flags && flags.response_in_millimeters) {
    returnedPose.setX(returnedPose.getX() / 1000);
    returnedPose.setY(returnedPose.getY() / 1000);
    returnedPose.setZ(returnedPose.getZ() / 1000);
  }
  const mapAndPose: MapAndPose = {
    map,
    pose: returnedPose,
  };
  return mapAndPose;
};

const handleRefresh2dResponse = (response: MapAndPose): void => {
  pointcloud = response.map;
  pose = response.pose;

  displayPose.x = Number(pose.getX().toFixed(1));
  displayPose.y = Number(pose.getY().toFixed(1));
  displayPose.z = Number(pose.getZ().toFixed(1));

  displayPose.ox = Number(pose.getOX().toFixed(1));
  displayPose.oy = Number(pose.getOY().toFixed(1));
  displayPose.oz = Number(pose.getOZ().toFixed(1));
  displayPose.th = Number(pose.getTheta().toFixed(1));

  pointCloudUpdateCount += 1;
};

const handleRefresh3dResponse = (response: Uint8Array): void => {
  pointcloud = response;
  pointCloudUpdateCount += 1;
};

const handleError = (errorLocation: string, error: unknown): void => {
  if (isServiceError(error)) {
    displayError(error as ServiceError);
  } else {
    displayError(`${errorLocation} hit error: ${error}`);
  }
};

const scheduleRefresh2d = (name: string, time: string) => {
  const timeoutCallback = async () => {
    try {
      const res = await refresh2d(name);
      handleRefresh2dResponse(res);
    } catch (error) {
      handleError('refresh2d', error);
      selected2dValue = 'manual';
      refreshErrorMessage2d = error !== null && typeof error === 'object' && 'message' in error
        ? `${refreshErrorMessage} ${error.message}`
        : `${refreshErrorMessage} ${error}`;
      return;
    }
    if (refresh2DCancelled) {
      return;
    }
    scheduleRefresh2d(name, time);
  };
  slam2dTimeoutId = window.setTimeout(timeoutCallback, Number.parseFloat(time) * 1000);
};

const scheduleRefresh3d = (name: string, time: string) => {
  const timeoutCallback = async () => {
    try {
      const res = await fetchSLAMMap(name);
      handleRefresh3dResponse(res);
    } catch (error) {
      handleError('fetchSLAMMap', error);
      selected3dValue = 'manual';
      refreshErrorMessage3d = error !== null && typeof error === 'object' && 'message' in error
        ? `${refreshErrorMessage} ${error.message}`
        : `${refreshErrorMessage} ${error}`;
      return;
    }
    if (refresh3DCancelled) {
      return;
    }
    scheduleRefresh3d(name, time);
  };
  slam3dTimeoutId = window.setTimeout(timeoutCallback, Number.parseFloat(time) * 1000);
};

const updateSLAM2dRefreshFrequency = async (name: string, time: 'manual' | string) => {
  refresh2DCancelled = true;
  window.clearTimeout(slam2dTimeoutId);
  refreshErrorMessage2d = null;
  refreshErrorMessage3d = null;

  if (time === 'manual') {
    try {
      const res = await refresh2d(name);
      handleRefresh2dResponse(res);
    } catch (error) {
      handleError('refresh2d', error);
      selected2dValue = 'manual';
      refreshErrorMessage2d = error !== null && typeof error === 'object' && 'message' in error
        ? `${refreshErrorMessage} ${error.message}`
        : `${refreshErrorMessage} ${error}`;
    }
  } else {
    refresh2DCancelled = false;
    scheduleRefresh2d(name, time);
  }
};

const updateSLAM3dRefreshFrequency = async (name: string, time: 'manual' | string) => {
  refresh3DCancelled = true;
  window.clearTimeout(slam3dTimeoutId);
  refreshErrorMessage2d = null;
  refreshErrorMessage3d = null;

  if (time === 'manual') {
    try {
      const res = await fetchSLAMMap(name);
      handleRefresh3dResponse(res);
    } catch (error) {
      handleError('fetchSLAMMap', error);
      selected3dValue = 'manual';
      refreshErrorMessage3d = error !== null && typeof error === 'object' && 'message' in error
        ? `${refreshErrorMessage} ${error.message}`
        : `${refreshErrorMessage} ${error}`;
    }
  } else {
    refresh3DCancelled = false;
    scheduleRefresh3d(name, time);
  }
};

const toggle3dExpand = () => {
  show3d = !show3d;
  if (!show3d) {
    selected3dValue = 'manual';
    return;
  }
  updateSLAM3dRefreshFrequency(props.name, selected3dValue);
};

const toggle2dExpand = () => {
  show2d = !show2d;
  if (!show2d) {
    selected2dValue = 'manual';
    return;
  }
  updateSLAM2dRefreshFrequency(props.name, selected2dValue);
};

const selectSLAM2dRefreshFrequency = () => {
  updateSLAM2dRefreshFrequency(props.name, selected2dValue);
};

const selectSLAMPCDRefreshFrequency = () => {
  updateSLAM3dRefreshFrequency(props.name, selected3dValue);
};

const refresh2dMap = () => {
  updateSLAM2dRefreshFrequency(props.name, 'manual');
};

const refresh3dMap = () => {
  updateSLAM3dRefreshFrequency(props.name, 'manual');
};

const handle2dRenderClick = (event: THREE.Vector3) => {
  updatedDest = true;
  destinationMarker = event;
};

const handleUpdateDestX = (event: CustomEvent<{ value: string }>) => {
  destinationMarker.x = Number.parseFloat(event.detail.value);
  updatedDest = true;
};

const handleUpdateDestY = (event: CustomEvent<{ value: string }>) => {
  destinationMarker.y = Number.parseFloat(event.detail.value);
  updatedDest = true;
};

const baseCopyPosition = () => {
  copyToClipboardWithToast(JSON.stringify(displayPose));
};

const toggleAxes = () => {
  showAxes = !showAxes;
};

onMounted(() => {
  props.statusStream?.on('end', () => {
    window.clearTimeout(slam2dTimeoutId);
    window.clearTimeout(slam3dTimeoutId);
  });
});

onUnmounted(() => {
  window.clearTimeout(slam2dTimeoutId);
  window.clearTimeout(slam3dTimeoutId);
});

</script>

<template>
  <v-collapse
    :title="props.name"
    class="slam"
    @toggle="toggle2dExpand()"
  >
    <v-breadcrumbs
      slot="title"
      crumbs="slam"
    />
    <v-button
      slot="header"
      variant="danger"
      icon="stop-circle"
      :disabled="moveClicked ? 'false' : 'true'"
      label="STOP"
      @click="stopMoveOnMap()"
    />
    <div class="flex flex-wrap gap-4 border border-t-0 border-medium sm:flex-nowrap">
      <div class="flex min-w-fit flex-col gap-4 p-4">
        <div class="float-left pb-4">
          <div class="flex">
            <div class="w-64">
              <p class="mb-1 font-bold text-gray-800">
                Map
              </p>
              <div class="relative">
                <p class="mb-1 text-xs text-gray-500 ">
                  Refresh frequency
                </p>
                <select
                  v-model="selected2dValue"
                  class="
                      m-0 w-full appearance-none border border-solid border-medium bg-white bg-clip-padding
                      px-3 py-1.5 text-xs font-normal text-default focus:outline-none
                    "
                  aria-label="Default select example"
                  @change="selectSLAM2dRefreshFrequency()"
                >
                  <option
                    value="manual"
                    class="pb-5"
                  >
                    Manual Refresh
                  </option>
                  <option value="30">
                    Every 30 seconds
                  </option>
                  <option value="10">
                    Every 10 seconds
                  </option>
                  <option value="5">
                    Every 5 seconds
                  </option>
                  <option value="1">
                    Every second
                  </option>
                </select>
                <div
                  class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-2"
                >
                  <svg
                    class="h-4 w-4 stroke-2 text-gray-700"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-linejoin="round"
                    stroke-linecap="round"
                    fill="none"
                  >
                    <path d="M18 16L12 22L6 16" />
                  </svg>
                </div>
              </div>
            </div>
            <div class="px-2 pt-11">
              <v-button
                label="Refresh"
                icon="refresh"
                @click="refresh2dMap()"
              />
            </div>
          </div>
          <hr class="my-4 border-t border-medium">
          <div class="flex flex-row">
            <p class="mb-1 pr-52 font-bold text-gray-800">
              Ending Position
            </p>
            <v-icon
              name="trash"
              @click="deleteDestinationMarker()"
            />
          </div>
          <div class="flex flex-row pb-2">
            <v-input
              type="number"
              label="x"
              incrementor="slider"
              :value="destinationMarker.x"
              step="0.1"
              @input="handleUpdateDestX($event)"
            />
            <v-input
              class="pl-2"
              type="number"
              label="y"
              incrementor="slider"
              :value="destinationMarker.y"
              step="0.1"
              @input="handleUpdateDestY($event)"
            />
          </div>
          <v-button
            class="pt-1"
            label="Move"
            variant="success"
            icon="play-circle-filled"
            :disabled="allowMove ? 'false' : 'true'"
            @click="moveOnMap()"
          />
          <v-switch
            class="pt-2"
            label="Show Axes"
            :value="showAxes ? 'on' : 'off'"
            @input="toggleAxes()"
          />
        </div>
      </div>
      <div class="gap-4x border-border-1 w-full justify-start sm:border-l">
        <div
          v-if="refreshErrorMessage2d && show2d"
          class="border-l-4 border-red-500 bg-gray-100 px-4 py-3"
        >
          {{ refreshErrorMessage2d }}
        </div>
        <div v-if="loaded2d && show2d">
          <div class="flex flex-row pl-5 pt-3">
            <div class="flex flex-col">
              <p class="text-xs">
                Current Position
              </p>
              <div class="flex flex-row items-center">
                <p class="items-end pr-2 text-xs text-gray-500">
                  x
                </p>
                <p>{{ displayPose.x }}</p>

                <p class="pl-9 pr-2 text-xs text-gray-500">
                  y
                </p>
                <p>{{ displayPose.y }}</p>

                <p class="pl-9 pr-2 text-xs text-gray-500">
                  z
                </p>
                <p>{{ displayPose.z }}</p>
              </div>
            </div>
            <div class="flex flex-col pl-10">
              <p class="text-xs">
                Current Orientation
              </p>
              <div class="flex flex-row items-center">
                <p class="pr-2 text-xs text-gray-500">
                  o<sub>x</sub>
                </p>
                <p>{{ displayPose.ox }}</p>

                <p class="pl-9 pr-2 text-xs text-gray-500">
                  o<sub>y</sub>
                </p>
                <p>{{ displayPose.oy }}</p>

                <p class="pl-9 pr-2 text-xs text-gray-500">
                  o<sub>z</sub>
                </p>
                <p>{{ displayPose.oz }}</p>

                <p class="pl-9 pr-2 text-xs text-gray-500">
                  &theta;
                </p>
                <p>{{ displayPose.th }}</p>
              </div>
            </div>
            <div class="pl-4 pt-2">
              <v-icon
                name="copy"
                @click="baseCopyPosition()"
              />
            </div>
          </div>
          <Slam2dRenderer
            :point-cloud-update-count="pointCloudUpdateCount"
            :pointcloud="pointcloud"
            :pose="pose"
            :name="name"
            :resources="resources"
            :client="client"
            :dest-exists="updatedDest"
            :dest-vector="destinationMarker"
            :axes-visible="showAxes"
            @click="handle2dRenderClick($event)"
          />
        </div>
      </div>
    </div>
    <div class="border border-medium border-t-transparent p-4 ">
      <v-switch
        label="View SLAM Map (3D)"
        :value="show3d ? 'on' : 'off'"
        @input="toggle3dExpand()"
      />
      <div
        v-if="refreshErrorMessage3d && show3d"
        class="border-l-4 border-red-500 bg-gray-100 px-4 py-3"
      >
        {{ refreshErrorMessage3d }}
      </div>
      <div class="flex items-end gap-2">
        <div
          v-if="show3d"
          class="w-56"
        >
          <p class="font-label mb-1 text-gray-800">
            Refresh frequency
          </p>
          <div class="relative">
            <select
              v-model="selected3dValue"
              class="
                      m-0 w-full appearance-none border border-solid border-medium bg-white
                      bg-clip-padding px-3 py-1.5 text-xs font-normal text-gray-700 focus:outline-none"
              aria-label="Default select example"
              @change="selectSLAMPCDRefreshFrequency()"
            >
              <option value="manual">
                Manual Refresh
              </option>
              <option value="30">
                Every 30 seconds
              </option>
              <option value="10">
                Every 10 seconds
              </option>
              <option value="5">
                Every 5 seconds
              </option>
              <option value="1">
                Every second
              </option>
            </select>
            <div
              class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-2"
            >
              <svg
                class="h-4 w-4 stroke-2 text-default"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-linejoin="round"
                stroke-linecap="round"
                fill="none"
              >
                <path d="M18 16L12 22L6 16" />
              </svg>
            </div>
          </div>
        </div>
        <v-button
          v-if="show3d"
          icon="refresh"
          label="Refresh"
          @click="refresh3dMap()"
        />
      </div>
      <PCD
        v-if="show3d"
        :resources="resources"
        :pointcloud="pointcloud"
        :client="client"
      />
    </div>
  </v-collapse>
</template>
