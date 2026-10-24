module top #(parameter W = 4) (input clk, input [W-1:0] data);
  logic [W-1:0] state;
  function int g(input int x);
    int tmp;
    return x + tmp;
  endfunction
  always_ff @(posedge clk) begin : blk
    logic [W-1:0] local_v;
    for (int j = 0; j < 2; j++) state <= data;
  end
  if (W > 1) begin : gen
    logic [3:0] inner;
  end
  if (W > 8) begin : other
    logic [3:0] inner;
  end
endmodule
class C;
  int value;
  function new(int value);
    this.value = value;
  endfunction
endclass
module second;
  logic state;
endmodule
