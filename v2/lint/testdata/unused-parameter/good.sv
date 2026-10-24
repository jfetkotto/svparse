module top #(parameter W = 8, parameter type T = logic, parameter unused_knob = 0) (input [W-1:0] d);
  localparam HALF = W / 2;
  localparam [3:0] MASK = 4'hF;
  T t;
  logic [HALF-1:0] lo = d[HALF-1:0] & MASK;
  leaf #(.N(HALF)) u();
endmodule
module leaf #(parameter N = 1); logic [N-1:0] v; endmodule
